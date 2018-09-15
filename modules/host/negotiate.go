package host

import (
	"net"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/ed25519"
)

var (
	// errBadContractOutputCounts is returned if the presented file contract
	// revision has the wrong number of outputs for either the valid or the
	// missed proof outputs.
	errBadContractOutputCounts = ErrorCommunication("rejected for having an unexpected number of outputs")

	// errBadContractParent is returned when a file contract revision is
	// presented which has a parent id that doesn't match the file contract
	// which is supposed to be getting revised.
	errBadContractParent = ErrorCommunication("could not find contract's parent")

	// errBadFileMerkleRoot is returned if the renter incorrectly updates the
	// file merkle root during a file contract revision.
	errBadFileMerkleRoot = ErrorCommunication("rejected for bad file merkle root")

	// errBadFileSize is returned if the renter incorrectly download and
	// changes the file size during a file contract revision.
	errBadFileSize = ErrorCommunication("rejected for bad file size")

	// errBadModificationIndex is returned if the renter requests a change on a
	// sector root that is not in the file contract.
	errBadModificationIndex = ErrorCommunication("renter has made a modification that points to a nonexistent sector")

	// errBadParentID is returned if the renter incorrectly download and
	// provides the wrong parent id during a file contract revision.
	errBadParentID = ErrorCommunication("rejected for bad parent id")

	// errBadPayoutUnlockHashes is returned if the renter incorrectly sets the
	// payout unlock hashes during contract formation.
	errBadPayoutUnlockHashes = ErrorCommunication("rejected for bad unlock hashes in the payout")

	// errBadRevisionNumber number is returned if the renter incorrectly
	// download and does not increase the revision number during a file
	// contract revision.
	errBadRevisionNumber = ErrorCommunication("rejected for bad revision number")

	// errBadSectorSize is returned if the renter provides a sector to be
	// inserted that is the wrong size.
	errBadSectorSize = ErrorCommunication("renter has provided an incorrectly sized sector")

	// errBadUnlockConditions is returned if the renter incorrectly download
	// and does not provide the right unlock conditions in the payment
	// revision.
	errBadUnlockConditions = ErrorCommunication("rejected for bad unlock conditions")

	// errBadUnlockHash is returned if the renter incorrectly updates the
	// unlock hash during a file contract revision.
	errBadUnlockHash = ErrorCommunication("rejected for bad new unlock hash")

	// errBadWindowEnd is returned if the renter incorrectly download and
	// changes the window end during a file contract revision.
	errBadWindowEnd = ErrorCommunication("rejected for bad new window end")

	// errBadWindowStart is returned if the renter incorrectly updates the
	// window start during a file contract revision.
	errBadWindowStart = ErrorCommunication("rejected for bad new window start")

	// errEarlyWindow is returned if the file contract provided by the renter
	// has a storage proof window that is starting too near in the future.
	errEarlyWindow = ErrorCommunication("rejected for a window that starts too soon")

	// errEmptyObject is returned if the renter sends an empty or nil object
	// unexpectedly.
	errEmptyObject = ErrorCommunication("renter has unexpectedly send an empty/nil object")

	// errHighRenterMissedOutput is returned if the renter incorrectly download
	// and deducts an insufficient amount from the renter missed outputs during
	// a file contract revision.
	errHighRenterMissedOutput = ErrorCommunication("rejected for high paying renter missed output")

	// errHighRenterValidOutput is returned if the renter incorrectly download
	// and deducts an insufficient amount from the renter valid outputs during
	// a file contract revision.
	errHighRenterValidOutput = ErrorCommunication("rejected for high paying renter valid output")

	// errIllegalOffsetAndLength is returned if the renter tries perform a
	// modify operation that uses a troublesome combination of offset and
	// length.
	errIllegalOffsetAndLength = ErrorCommunication("renter is trying to do a modify with an illegal offset and length")

	// errLargeSector is returned if the renter sends a RevisionAction that has
	// data which creates a sector that is larger than what the host uses.
	errLargeSector = ErrorCommunication("renter has sent a sector that exceeds the host's sector size")

	// errLateRevision is returned if the renter is attempting to revise a
	// revision after the revision deadline. The host needs time to submit the
	// final revision to the blockchain to guarantee payment, and therefore
	// will not accept revisions once the window start is too close.
	errLateRevision = ErrorCommunication("renter is requesting revision after the revision deadline")

	// errLongDuration is returned if the renter proposes a file contract with
	// an experation that is too far into the future according to the host's
	// settings.
	errLongDuration = ErrorCommunication("renter proposed a file contract with a too-long duration")

	// errLowHostMissedOutput is returned if the renter incorrectly updates the
	// host missed proof output during a file contract revision.
	errLowHostMissedOutput = ErrorCommunication("rejected for low paying host missed output")

	// errLowHostValidOutput is returned if the renter incorrectly updates the
	// host valid proof output during a file contract revision.
	errLowHostValidOutput = ErrorCommunication("rejected for low paying host valid output")

	// errLowTransactionFees is returned if the renter provides a transaction
	// that the host does not feel is able to make it onto the blockchain.
	errLowTransactionFees = ErrorCommunication("rejected for including too few transaction fees")

	// errLowVoidOutput is returned if the renter has not allocated enough
	// funds to the void output.
	errLowVoidOutput = ErrorCommunication("rejected for low value void output")

	// errMismatchedHostPayouts is returned if the renter incorrectly sets the
	// host valid and missed payouts to different values during contract
	// formation.
	errMismatchedHostPayouts = ErrorCommunication("rejected because host valid and missed payouts are not the same value")

	// errSmallWindow is returned if the renter suggests a storage proof window
	// that is too small.
	errSmallWindow = ErrorCommunication("rejected for small window size")

	// errUnknownModification is returned if the host receives a modification
	// action from the renter that it does not understand.
	errUnknownModification = ErrorCommunication("renter is attempting an action that the host does not understand")
)

// managedFinalizeContract will take a file contract, add the host's
// collateral, and then try submitting the file contract to the transaction
// pool. If there is no error, the completed transaction set will be returned
// to the caller.
// TODO joint signatures
func (h *Host) managedFinalizeContract(jointSecretKey crypto.SecretKey, fullTxnSet []types.Transaction, revisionTransaction types.Transaction, builder modules.TransactionBuilder, initialSectorRoots []crypto.Hash, hostCollateral, hostInitialRevenue, hostInitialRisk types.Currency, settings modules.HostExternalSettings) ([]types.TransactionSignature, types.FileContractID, error) {
	// Create and add the storage obligation for this file contract.
	fullTxn, _ := builder.View()
	so := storageObligation{
		SectorRoots: initialSectorRoots,

		JointSecretKey:          jointSecretKey,

		ContractCost:            settings.ContractPrice,
		LockedCollateral:        hostCollateral,
		PotentialStorageRevenue: hostInitialRevenue,
		RiskedCollateral:        hostInitialRisk,

		OriginTransactionSet:   fullTxnSet,
		RevisionTransactionSet: []types.Transaction{revisionTransaction},
	}

	// Get a lock on the storage obligation.
	lockErr := h.managedTryLockStorageObligation(so.id())
	if lockErr != nil {
		build.Critical("failed to get a lock on a brand new storage obligation")
		return nil, types.FileContractID{}, lockErr
	}
	var err error
	defer func() {
		if err != nil {
			h.managedUnlockStorageObligation(so.id())
		}
	}()

	// addStorageObligation will submit the transaction to the transaction
	// pool, and will only do so if there was not some error in creating the
	// storage obligation. If the transaction pool returns a consensus
	// conflict, wait 30 seconds and try again.
	err = func() error {
		// Try adding the storage obligation. If there's an error, wait a few
		// seconds and try again. Eventually time out. It should be noted that
		// the storage obligation locking is both crappy and incomplete, and
		// that I'm not sure how this timeout plays with the overall host
		// timeouts.
		//
		// The storage obligation locks should occur at the highest level, not
		// just when the actual modification is happening.
		i := 0
		for {
			err = h.managedAddStorageObligation(so)
			if err == nil {
				return nil
			}
			if err != nil && i > 4 {
				h.log.Println(err)
				builder.Drop()
				return err
			}

			i++
			if build.Release == "standard" {
				time.Sleep(time.Second * 15)
			}
		}
	}()
	if err != nil {
		return nil, types.FileContractID{}, err
	}

	// Get the host's transaction signatures from the builder.
	var hostTxnSignatures []types.TransactionSignature
	_, _, txnSigIndices := builder.ViewAdded()
	for _, sigIndex := range txnSigIndices {
		hostTxnSignatures = append(hostTxnSignatures, fullTxn.TransactionSignatures[sigIndex])
	}
	return hostTxnSignatures, so.id(), nil
}

func (h *Host) managedNegotiateRevisionSignature(conn net.Conn, revisionTransaction types.Transaction, secretKey, jointSecretKey crypto.SecretKey, blockHeight types.BlockHeight) (types.Transaction, error) {
	privateKey := make([]byte, ed25519.PrivateKeySize)
	jointPrivateKey := make([]byte, ed25519.PrivateKeySize)
	copy(privateKey[:], secretKey[:])
	copy(jointPrivateKey[:], jointSecretKey[:])
	msg := revisionTransaction.SigHash(0)
	hostNoncePoint := ed25519.GenerateNoncePoint(privateKey, msg[:])

	// Extend the deadline to meet the nonce point negotiation.
	conn.SetDeadline(time.Now().Add(modules.NegotiateCurvePointTime))
	// Grab the renter's nonce point of the contract transaction so that we can make a joint
	// signature
	var renterNoncePoint ed25519.CurvePoint
	err := modules.ReadCurvePoint(conn, &renterNoncePoint)
	if err != nil {
		return revisionTransaction, extendErr("could not read renter nonce point: ", ErrorConnection(err.Error()))
	}

	// Send our contract tx nonce point to the renter
	if err = modules.WriteCurvePoint(conn, hostNoncePoint); err != nil {
		return revisionTransaction, extendErr("could not send our contract transaction nonce point: ", ErrorConnection(err.Error()))
	}

	renterRevisionSignature := make([]byte, ed25519.SignatureSize)
	err = modules.ReadSignature(conn, renterRevisionSignature)
	if err != nil {
		return revisionTransaction, extendErr("could not read renter revision signatures: ", ErrorConnection(err.Error()))
	}

	noncePoints := []ed25519.CurvePoint{hostNoncePoint, renterNoncePoint}
	hostRevisionSignature := ed25519.JointSign(privateKey, jointPrivateKey, noncePoints, msg[:])
	aggregateSignature := ed25519.AddSignature(hostRevisionSignature, renterRevisionSignature)
	revisionTransaction.TransactionSignatures[0].Signature = aggregateSignature

	fcr := revisionTransaction.FileContractRevisions[0]
	err = modules.VerifyFileContractRevisionTransactionSignatures(fcr, revisionTransaction.TransactionSignatures, blockHeight)
	if err != nil {
		return revisionTransaction, extendErr("could not verify revision signature: ", ErrorConnection(err.Error()))
	}
	return revisionTransaction, nil
}
