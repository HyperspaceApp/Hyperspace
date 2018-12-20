package contractor

import (
	"errors"
	"sync"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/proto"
	"github.com/HyperspaceApp/Hyperspace/types"
)

var errInvalidDownloader = errors.New("downloader has been invalidated because its contract is being renewed")

// An Downloader retrieves sectors from with a host. It requests one sector at
// a time, and revises the file contract to transfer money to the host
// proportional to the data retrieved.
type Downloader interface {
	// Download requests the specified sector data.
	Download(root crypto.Hash, offset, length uint32) ([]byte, error)

	// Sector retrieves the sector with the specified Merkle root, and revises
	// the underlying contract to pay the host proportionally to the data
	// retrieve.
	Sector(root crypto.Hash) ([]byte, error)

	// Close terminates the connection to the host.
	Close() error
}

// A hostDownloader retrieves sectors by calling the download RPC on a host.
// It implements the Downloader interface. hostDownloaders are safe for use by
// multiple goroutines.
type hostDownloader struct {
	clients      int // safe to Close when 0
	contractID   types.FileContractID
	contractor   *Contractor
	downloader   *proto.Downloader
	hostSettings modules.HostExternalSettings
	invalid      bool   // true if invalidate has been called
	speed        uint64 // Bytes per second.
	mu           sync.Mutex
}

// invalidate sets the invalid flag and closes the underlying
// proto.Downloader. Once invalidate returns, the hostDownloader is guaranteed
// to not further revise its contract. This is used during contract renewal to
// prevent a Downloader from revising a contract mid-renewal.
func (hd *hostDownloader) invalidate() {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	if !hd.invalid {
		hd.downloader.Close()
		hd.invalid = true
	}
	hd.contractor.mu.Lock()
	delete(hd.contractor.downloaders, hd.contractID)
	hd.contractor.mu.Unlock()
}

// Close cleanly terminates the download loop with the host and closes the
// connection.
func (hd *hostDownloader) Close() error {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	hd.clients--
	// Close is a no-op if invalidate has been called, or if there are other
	// clients still using the hostDownloader.
	if hd.invalid || hd.clients > 0 {
		return nil
	}
	hd.invalid = true
	hd.contractor.mu.Lock()
	delete(hd.contractor.downloaders, hd.contractID)
	hd.contractor.mu.Unlock()
	return hd.downloader.Close()
}

// HostSettings returns the settings of the host that the downloader connects
// to.
func (hd *hostDownloader) HostSettings() modules.HostExternalSettings {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	return hd.hostSettings
}

// Sector retrieves the sector with the specified Merkle root, and revises
// the underlying contract to pay the host proportionally to the data
// retrieve.
func (hd *hostDownloader) Sector(root crypto.Hash) ([]byte, error) {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	if hd.invalid {
		return nil, errInvalidDownloader
	}

	// Download the sector.
	_, sector, err := hd.downloader.Sector(root)
	if err != nil {
		return nil, err
	}
	return sector, nil
}

// Download retrieves the requested sector data and revises the underlying
// contract to pay the host proportionally to the data retrieved.
func (hd *hostDownloader) Download(root crypto.Hash, offset, length uint32) ([]byte, error) {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	if hd.invalid {
		return nil, errInvalidDownloader
	}
	_, data, err := hd.downloader.Download(root, offset, length)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Downloader returns a Downloader object that can be used to download sectors
// from a host.
func (c *Contractor) Downloader(pk types.SiaPublicKey, cancel <-chan struct{}) (_ Downloader, err error) {
	c.mu.RLock()
	id, gotID := c.pubKeysToContractID[string(pk.Key)]
	cachedDownloader, haveDownloader := c.downloaders[id]
	cachedSession, haveSession := c.sessions[id]
	height := c.blockHeight
	renewing := c.renewing[id]
	c.mu.RUnlock()
	if !gotID {
		return nil, errors.New("failed to get filecontract id from key")
	}
	if renewing {
		return nil, errors.New("currently renewing that contract")
	} else if haveDownloader {
		// increment number of clients and return
		cachedDownloader.mu.Lock()
		cachedDownloader.clients++
		cachedDownloader.mu.Unlock()
		return cachedDownloader, nil
	} else if haveSession {
		cachedSession.mu.Lock()
		cachedSession.clients++
		cachedSession.mu.Unlock()
		return cachedSession, nil
	}

	// Fetch the contract and host.
	contract, haveContract := c.staticContracts.View(id)
	if !haveContract {
		return nil, errors.New("no record of that contract")
	}
	host, haveHost := c.hdb.Host(contract.HostPublicKey)
	if height > contract.EndHeight {
		return nil, errors.New("contract has already ended")
	} else if !haveHost {
		return nil, errors.New("no record of that host")
	} else if host.DownloadBandwidthPrice.Cmp(maxDownloadPrice) > 0 {
		return nil, errTooExpensive
	}

	// If host is >= 1.4.0, use the new renter-host protocol.
	if build.VersionCmp(host.Version, "1.4.0") >= 0 {
		return c.Session(pk, cancel)
	}

	// create downloader
	d, err := c.staticContracts.NewDownloader(host, contract.ID, c.hdb, cancel)
	if err != nil {
		return nil, err
	}

	// cache downloader
	hd := &hostDownloader{
		clients:    1,
		contractor: c,
		downloader: d,
		contractID: id,
	}
	c.mu.Lock()
	c.downloaders[contract.ID] = hd
	c.mu.Unlock()

	return hd, nil
}
