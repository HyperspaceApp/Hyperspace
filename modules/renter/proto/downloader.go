package proto

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/node/api/client"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/HyperspaceApp/errors"
)

// A Downloader retrieves sectors by calling the download RPC on a host.
// Downloaders are NOT thread- safe; calls to Sector must be serialized.
type Downloader struct {
	closeChan   chan struct{}
	conn        net.Conn
	contractID  types.FileContractID
	contractSet *ContractSet
	deps        modules.Dependencies
	hdb         hostDB
	host        modules.HostDBEntry
	once        sync.Once
	httpClient  *client.Client
}

// Sector retrieves the sector with the specified Merkle root, and revises
// the underlying contract to pay the host proportionally to the data
// retrieve.
func (hd *Downloader) Sector(root crypto.Hash) (_ modules.RenterContract, _ []byte, err error) {
	// Reset deadline when finished.
	defer extendDeadline(hd.conn, time.Hour) // TODO: Constant.

	// Acquire the contract.
	// TODO: why not just lock the SafeContract directly?
	sc, haveContract := hd.contractSet.Acquire(hd.contractID)
	if !haveContract {
		return modules.RenterContract{}, nil, errors.New("contract not present in contract set")
	}
	defer hd.contractSet.Return(sc)
	contract := sc.header // for convenience

	// calculate price
	sectorPrice := hd.host.DownloadBandwidthPrice.Mul64(modules.SectorSize)
	if contract.RenterFunds().Cmp(sectorPrice) < 0 {
		return modules.RenterContract{}, nil, errors.New("contract has insufficient funds to support download")
	}
	// To mitigate small errors (e.g. differing block heights), fudge the
	// price and collateral by 0.2%.
	sectorPrice = sectorPrice.MulFloat(1 + hostPriceLeeway)

	// create the download revision
	rev := newDownloadRevision(contract.LastRevision(), sectorPrice)

	// initiate download by confirming host settings
	extendDeadline(hd.conn, modules.NegotiateSettingsTime)
	if err := startDownload(hd.conn, hd.host); err != nil {
		return modules.RenterContract{}, nil, err
	}

	// record the change we are about to make to the contract. If we lose power
	// mid-revision, this allows us to restore either the pre-revision or
	// post-revision contract.
	walTxn, err := sc.recordDownloadIntent(rev, sectorPrice)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// send download action
	extendDeadline(hd.conn, 2*time.Minute) // TODO: Constant.
	err = encoding.WriteObject(hd.conn, []modules.DownloadAction{{
		MerkleRoot: root,
		Offset:     0,
		Length:     modules.SectorSize,
	}})
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Increase Successful/Failed interactions accordingly
	defer func() {
		// Ignore ErrStopResponse and closed network connecton errors since
		// they are not considered a failed interaction with the host.
		if err != nil && err != modules.ErrStopResponse && !strings.Contains(err.Error(), "use of closed network connection") {
			hd.hdb.IncrementFailedInteractions(contract.HostPublicKey())
			err = errors.Extend(err, modules.ErrHostFault)
		} else {
			hd.hdb.IncrementSuccessfulInteractions(contract.HostPublicKey())
		}
	}()

	// Disrupt before sending the signed revision to the host.
	if hd.deps.Disrupt("InterruptDownloadBeforeSendingRevision") {
		return modules.RenterContract{}, nil,
			errors.New("InterruptDownloadBeforeSendingRevision disrupt")
	}

	// send the revision to the host for approval
	extendDeadline(hd.conn, connTimeout)
	signedTxn, err := negotiateRevision(hd.conn, rev, func(sig crypto.Hash, txn types.Transaction) (crypto.Signature, error) {
		if hd.httpClient == nil {
			return crypto.SignHash(sig, contract.SecretKey), nil
		}
		// sign remotely
		return hd.httpClient.ThirdpartyServerSignPost(hd.contractID, txn)
	})
	if err == modules.ErrStopResponse {
		// If the host wants to stop communicating after this iteration, close
		// our connection; this will cause the next download to fail. However,
		// we must delay closing until we've finished downloading the sector.
		defer hd.conn.Close()
	} else if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Disrupt after sending the signed revision to the host.
	if hd.deps.Disrupt("InterruptDownloadAfterSendingRevision") {
		return modules.RenterContract{}, nil,
			errors.New("InterruptDownloadAfterSendingRevision disrupt")
	}

	// read sector data, completing one iteration of the download loop
	extendDeadline(hd.conn, modules.NegotiateDownloadTime)
	var sectors [][]byte
	if err := encoding.ReadObject(hd.conn, &sectors, modules.SectorSize+16); err != nil {
		return modules.RenterContract{}, nil, err
	} else if len(sectors) != 1 {
		return modules.RenterContract{}, nil, errors.New("host did not send enough sectors")
	}
	sector := sectors[0]
	if uint64(len(sector)) != modules.SectorSize {
		return modules.RenterContract{}, nil, errors.New("host did not send enough sector data")
	} else if crypto.MerkleRoot(sector) != root {
		return modules.RenterContract{}, nil, errors.New("host sent bad sector data")
	}

	// update contract and metrics
	if err := sc.commitDownload(walTxn, signedTxn, sectorPrice); err != nil {
		return modules.RenterContract{}, nil, err
	}

	return sc.Metadata(), sector, nil
}

// Download retrieves the requested sector data and revises the underlying
// contract to pay the host proportionally to the data retrieved.
func (hd *Downloader) Download(root crypto.Hash, offset, length uint32) (_ modules.RenterContract, _ []byte, err error) {
	// Reset deadline when finished.
	defer extendDeadline(hd.conn, time.Hour) // TODO: Constant.

	if uint64(offset)+uint64(length) > modules.SectorSize {
		return modules.RenterContract{}, nil, errors.New("illegal offset and/or length")
	}

	// Acquire the contract.
	// TODO: why not just lock the SafeContract directly?
	sc, haveContract := hd.contractSet.Acquire(hd.contractID)
	if !haveContract {
		return modules.RenterContract{}, nil, errors.New("contract not present in contract set")
	}
	defer hd.contractSet.Return(sc)
	contract := sc.header // for convenience

	// calculate price
	sectorPrice := hd.host.DownloadBandwidthPrice.Mul64(modules.SectorSize)
	if contract.RenterFunds().Cmp(sectorPrice) < 0 {
		return modules.RenterContract{}, nil, errors.New("contract has insufficient funds to support download")
	}
	// To mitigate small errors (e.g. differing block heights), fudge the
	// price and collateral by 0.2%.
	sectorPrice = sectorPrice.MulFloat(1 + hostPriceLeeway)

	// create the download revision
	rev := newDownloadRevision(contract.LastRevision(), sectorPrice)

	// initiate download by confirming host settings
	extendDeadline(hd.conn, modules.NegotiateSettingsTime)
	if err := startDownload(hd.conn, hd.host); err != nil {
		return modules.RenterContract{}, nil, err
	}

	// record the change we are about to make to the contract. If we lose power
	// mid-revision, this allows us to restore either the pre-revision or
	// post-revision contract.
	walTxn, err := sc.recordDownloadIntent(rev, sectorPrice)
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// send download action
	extendDeadline(hd.conn, 2*time.Minute) // TODO: Constant.
	err = encoding.WriteObject(hd.conn, []modules.DownloadAction{{
		MerkleRoot: root,
		Offset:     0,
		Length:     modules.SectorSize,
	}})
	if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Increase Successful/Failed interactions accordingly
	defer func() {
		// Ignore ErrStopResponse and closed network connecton errors since
		// they are not considered a failed interaction with the host.
		if err != nil && err != modules.ErrStopResponse && !strings.Contains(err.Error(), "use of closed network connection") {
			hd.hdb.IncrementFailedInteractions(contract.HostPublicKey())
			err = errors.Extend(err, modules.ErrHostFault)
		} else {
			hd.hdb.IncrementSuccessfulInteractions(contract.HostPublicKey())
		}
	}()

	// Disrupt before sending the signed revision to the host.
	if hd.deps.Disrupt("InterruptDownloadBeforeSendingRevision") {
		return modules.RenterContract{}, nil,
			errors.New("InterruptDownloadBeforeSendingRevision disrupt")
	}

	// send the revision to the host for approval
	extendDeadline(hd.conn, connTimeout)
	signedTxn, err := negotiateRevision(hd.conn, rev, func(sig crypto.Hash, txn types.Transaction) (crypto.Signature, error) {
		if hd.httpClient == nil {
			return crypto.SignHash(sig, contract.SecretKey), nil
		}
		// sign remotely
		return hd.httpClient.ThirdpartyServerSignPost(hd.contractID, txn)
	})
	if err == modules.ErrStopResponse {
		// If the host wants to stop communicating after this iteration, close
		// our connection; this will cause the next download to fail. However,
		// we must delay closing until we've finished downloading the sector.
		defer hd.conn.Close()
	} else if err != nil {
		return modules.RenterContract{}, nil, err
	}

	// Disrupt after sending the signed revision to the host.
	if hd.deps.Disrupt("InterruptDownloadAfterSendingRevision") {
		return modules.RenterContract{}, nil,
			errors.New("InterruptDownloadAfterSendingRevision disrupt")
	}

	// read sector data, completing one iteration of the download loop
	extendDeadline(hd.conn, modules.NegotiateDownloadTime)
	var sectors [][]byte
	if err := encoding.ReadObject(hd.conn, &sectors, modules.SectorSize+16); err != nil {
		return modules.RenterContract{}, nil, err
	} else if len(sectors) != 1 {
		return modules.RenterContract{}, nil, errors.New("host did not send enough sectors")
	}
	sector := sectors[0]
	if uint64(len(sector)) != modules.SectorSize {
		return modules.RenterContract{}, nil, errors.New("host did not send enough sector data")
	} else if crypto.MerkleRoot(sector) != root {
		return modules.RenterContract{}, nil, errors.New("host sent bad sector data")
	}

	// update contract and metrics
	if err := sc.commitDownload(walTxn, signedTxn, sectorPrice); err != nil {
		return modules.RenterContract{}, nil, err
	}

	// return the subset of requested data
	return sc.Metadata(), sector[offset:][:length], nil
}

// shutdown terminates the revision loop and signals the goroutine spawned in
// NewDownloader to return.
func (hd *Downloader) shutdown() {
	extendDeadline(hd.conn, modules.NegotiateSettingsTime)
	// don't care about these errors
	_, _ = verifySettings(hd.conn, hd.host)
	_ = modules.WriteNegotiationStop(hd.conn)
	close(hd.closeChan)
}

// Close cleanly terminates the download loop with the host and closes the
// connection.
func (hd *Downloader) Close() error {
	// using once ensures that Close is idempotent
	hd.once.Do(hd.shutdown)
	return hd.conn.Close()
}

// NewDownloader initiates the download request loop with a host, and returns a
// Downloader.
func (cs *ContractSet) NewDownloader(host modules.HostDBEntry, id types.FileContractID, hdb hostDB, httpClient *client.Client, cancel <-chan struct{}) (_ *Downloader, err error) {
	sc, ok := cs.Acquire(id)
	if !ok {
		return nil, errors.New("invalid contract")
	}
	defer cs.Return(sc)
	contract := sc.header

	// check that contract has enough value to support a download
	sectorPrice := host.DownloadBandwidthPrice.Mul64(modules.SectorSize)
	if contract.RenterFunds().Cmp(sectorPrice) < 0 {
		return nil, errors.New("contract has insufficient funds to support download")
	}

	// Increase Successful/Failed interactions accordingly
	defer func() {
		// A revision mismatch might not be the host's fault.
		if err != nil && !IsRevisionMismatch(err) {
			hdb.IncrementFailedInteractions(contract.HostPublicKey())
			err = errors.Extend(err, modules.ErrHostFault)
		} else if err == nil {
			hdb.IncrementSuccessfulInteractions(contract.HostPublicKey())
		}
	}()

	conn, closeChan, err := initiateRevisionLoop(host, sc, modules.RPCDownload, cancel, cs.rl)
	if err != nil {
		return nil, errors.AddContext(err, "failed to initiate revision loop")
	}
	// if we succeeded, we can safely discard the unappliedTxns
	for _, txn := range sc.unappliedTxns {
		txn.SignalUpdatesApplied()
	}
	sc.unappliedTxns = nil

	// the host is now ready to accept revisions
	return &Downloader{
		contractID:  id,
		contractSet: cs,
		host:        host,
		conn:        conn,
		closeChan:   closeChan,
		deps:        cs.deps,
		hdb:         hdb,
		httpClient:  httpClient,
	}, nil
}
