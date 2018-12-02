package proto

import (
	"net"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/HyperspaceApp/ed25519"
	"github.com/HyperspaceApp/errors"
)

// Renew negotiates a new contract for data already stored with a host, and
// submits the new contract transaction to tpool. The new contract is added to
// the ContractSet and its metadata is returned.
func (cs *ContractSet) Renew(oldContract *SafeContract, params ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, err error) {
	// for convenience
	contract := oldContract.header

	// Extract vars from params, for convenience.
	allowance, host, funding, startHeight, endHeight, refundAddress := params.Allowance, params.Host, params.Funding, params.StartHeight, params.EndHeight, params.RefundAddress
	ourSK := contract.SecretKey
	ourPK := ourSK.PublicKey()
        var renterPK, hostPK ed25519.PublicKey
        copy(hostPK[:], host.PublicKey.Key[:])
        copy(renterPK[:], ourPK[:])
        publicKeys := []ed25519.PublicKey{hostPK, renterPK}
        jointPublicKey, _, err := ed25519.GenerateJointKey(publicKeys)
	if err != nil {
		return modules.RenterContract{}, errors.New("unable to build joint public key during contract renewal")
	}
	var jointPK crypto.PublicKey
	copy(jointPK[:], jointPublicKey[:])
	lastRev := contract.LastRevision()

	// Calculate additional basePrice and baseCollateral. If the contract height
	// did not increase, basePrice and baseCollateral are zero.
	var basePrice, baseCollateral types.Currency
	if endHeight+host.WindowSize > lastRev.NewWindowEnd {
		timeExtension := uint64((endHeight + host.WindowSize) - lastRev.NewWindowEnd)
		basePrice = host.StoragePrice.Mul64(lastRev.NewFileSize).Mul64(timeExtension)    // cost of already uploaded data that needs to be covered by the renewed contract.
		baseCollateral = host.Collateral.Mul64(lastRev.NewFileSize).Mul64(timeExtension) // same as basePrice.
	}

	// Calculate the anticipated transaction fee.
	_, maxFee := tpool.FeeEstimation()
	txnFee := maxFee.Mul64(modules.EstimatedFileContractTransactionSetSize)

	// Calculate the payouts for the renter, host, and whole contract.
	period := endHeight - startHeight
	renterPayout, hostPayout, hostCollateral, err := modules.RenterPayouts(host, funding, txnFee, basePrice, baseCollateral, period, allowance.ExpectedStorage/allowance.Hosts)
	if err != nil {
		return modules.RenterContract{}, err
	}
	totalPayout := renterPayout.Add(hostPayout)

	if hostCollateral.Cmp(baseCollateral) < 0 {
		baseCollateral = hostCollateral
	}
	// check for negative currency
	if totalPayout.Cmp(hostPayout) < 0 {
		return modules.RenterContract{}, errors.New("insufficient funds to pay both siafund fee and also host payout")
	}

	// create file contract
	fc := types.FileContract{
		FileSize:       lastRev.NewFileSize,
		FileMerkleRoot: lastRev.NewFileMerkleRoot,
		WindowStart:    endHeight,
		WindowEnd:      endHeight + host.WindowSize,
		Payout:         totalPayout,
		UnlockHash:     lastRev.NewUnlockHash,
		RevisionNumber: 0,
		ValidProofOutputs: []types.SiacoinOutput{
			// renter
			{Value: totalPayout.Sub(hostPayout), UnlockHash: refundAddress},
			// host
			{Value: hostPayout, UnlockHash: host.UnlockHash},
		},
		MissedProofOutputs: []types.SiacoinOutput{
			// renter
			{Value: totalPayout.Sub(hostPayout), UnlockHash: refundAddress},
			// host gets its unused collateral back, plus the contract price
			{Value: hostCollateral.Sub(baseCollateral).Add(host.ContractPrice), UnlockHash: host.UnlockHash},
			// void gets the spent storage fees, plus the collateral being risked
			{Value: basePrice.Add(baseCollateral), UnlockHash: types.UnlockHash{}},
		},
	}

	// build transaction containing fc
	err = txnBuilder.FundSiacoins(funding)
	if err != nil {
		return modules.RenterContract{}, err
	}
	txnBuilder.AddFileContract(fc)
	// add miner fee
	txnBuilder.AddMinerFee(txnFee)

	// Create initial transaction set.
	txn, parentTxns := txnBuilder.View()
	unconfirmedParents, err := txnBuilder.UnconfirmedParents()
	if err != nil {
		return modules.RenterContract{}, err
	}
	txnSet := append(unconfirmedParents, append(parentTxns, txn)...)

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

	// initiate connection
	dialer := &net.Dialer{
		Cancel:  cancel,
		Timeout: connTimeout,
	}
	conn, err := dialer.Dial("tcp", string(host.NetAddress))
	if err != nil {
		return modules.RenterContract{}, err
	}
	defer func() { _ = conn.Close() }()

	// allot time for sending RPC ID, verifyRecentRevision, and verifySettings
	extendDeadline(conn, modules.NegotiateRecentRevisionTime+modules.NegotiateSettingsTime)
	if err = encoding.WriteObject(conn, modules.RPCRenewContract); err != nil {
		return modules.RenterContract{}, errors.New("couldn't initiate RPC: " + err.Error())
	}
	// verify that both parties are renewing the same contract
	err = verifyRecentRevision(conn, oldContract, host.Version)
	if err != nil {
		return modules.RenterContract{}, err
	}

	// verify the host's settings and confirm its identity
	host, err = verifySettings(conn, host)
	if err != nil {
		return modules.RenterContract{}, errors.New("settings exchange failed: " + err.Error())
	}
	if !host.AcceptingContracts {
		return modules.RenterContract{}, errors.New("host is not accepting contracts")
	}

	// allot time for negotiation
	extendDeadline(conn, modules.NegotiateRenewContractTime)

	// send acceptance, txn signed by us, and pubkey
	if err = modules.WriteNegotiationAcceptance(conn); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send initial acceptance: " + err.Error())
	}
	if err = encoding.WriteObject(conn, txnSet); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send the contract signed by us: " + err.Error())
	}
	if err = encoding.WriteObject(conn, ourPK); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send our public key: " + err.Error())
	}

	// read acceptance and txn signed by host
	if err = modules.ReadNegotiationAcceptance(conn); err != nil {
		return modules.RenterContract{}, errors.New("host did not accept our proposed contract: " + err.Error())
	}
	// host now sends any new parent transactions and inputs that
	// were added to the transaction
	var newParents []types.Transaction
	var newInputs []types.SiacoinInput
	if err = encoding.ReadObject(conn, &newParents, types.BlockSizeLimit); err != nil {
		return modules.RenterContract{}, errors.New("couldn't read the host's added parents: " + err.Error())
	}
	if err = encoding.ReadObject(conn, &newInputs, types.BlockSizeLimit); err != nil {
		return modules.RenterContract{}, errors.New("couldn't read the host's added inputs: " + err.Error())
	}

	// merge txnAdditions with txnSet
	txnBuilder.AddParents(newParents)
	for _, input := range newInputs {
		txnBuilder.AddSiacoinInput(input)
	}

	// sign the txn
	signedTxnSet, err := txnBuilder.Sign(true)
	if err != nil {
		return modules.RenterContract{}, modules.WriteNegotiationRejection(conn, errors.New("failed to sign transaction: "+err.Error()))
	}

	// calculate signatures added by the transaction builder
	var addedSignatures []types.TransactionSignature
	_, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet)-1].TransactionSignatures[i])
	}

	fcid := signedTxnSet[len(signedTxnSet)-1].FileContractID(0)
	// create initial (no-op) revision, transaction, and signature
	revisionTxn := modules.BuildInitialRevisionTransaction(fc, fcid, jointPK)
	revisionTxn, err = negotiateRevision(conn, revisionTxn.FileContractRevisions[0], ourSK, contract.JointSecretKey)
	if err != nil {
		return modules.RenterContract{}, errors.New("couldn't negotiate transaction revision: " + err.Error())
	}

	// Send acceptance and signatures
	if err = modules.WriteNegotiationAcceptance(conn); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send transaction acceptance: " + err.Error())
	}
	if err = encoding.WriteObject(conn, addedSignatures); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send added signatures: " + err.Error())
	}

	// Read the host acceptance and signatures.
	err = modules.ReadNegotiationAcceptance(conn)
	if err != nil {
		return modules.RenterContract{}, errors.New("host did not accept our signatures: " + err.Error())
	}
	var hostSigs []types.TransactionSignature
	if err = encoding.ReadObject(conn, &hostSigs, 2e3); err != nil {
		return modules.RenterContract{}, errors.New("couldn't read the host's signatures: " + err.Error())
	}
	for _, sig := range hostSigs {
		txnBuilder.AddTransactionSignature(sig)
	}
	var hostRevisionSig types.TransactionSignature
	if err = encoding.ReadObject(conn, &hostRevisionSig, 2e3); err != nil {
		return modules.RenterContract{}, errors.New("couldn't read the host's revision signature: " + err.Error())
	}
	revisionTxn.TransactionSignatures = append(revisionTxn.TransactionSignatures, hostRevisionSig)

	// Construct the final transaction.
	txn, parentTxns = txnBuilder.View()
	txnSet = append(parentTxns, txn)

	// Submit to blockchain.
	err = tpool.AcceptTransactionSet(txnSet)
	if err == modules.ErrDuplicateTransactionSet {
		// as long as it made it into the transaction pool, we're good
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Construct contract header.
	header := contractHeader{
		Transaction:     revisionTxn,
		SecretKey:       ourSK,
		StartHeight:     startHeight,
		TotalCost:       funding,
		ContractFee:     host.ContractPrice,
		TxnFee:          txnFee,
		StorageSpending: basePrice,
		Utility: modules.ContractUtility{
			GoodForUpload: true,
			GoodForRenew:  true,
		},
	}

	// Get old roots
	oldRoots, err := oldContract.merkleRoots.merkleRoots()
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Add contract to set.
	meta, err := cs.managedInsertContract(header, oldRoots)
	if err != nil {
		return modules.RenterContract{}, err
	}
	return meta, nil
}
