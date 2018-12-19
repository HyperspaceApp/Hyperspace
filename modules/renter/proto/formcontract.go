package proto

import (
	"net"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/HyperspaceApp/errors"
)

// FormContract forms a contract with a host and submits the contract
// transaction to tpool. The contract is added to the ContractSet and its
// metadata is returned.
func (cs *ContractSet) FormContract(params ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, err error) {
	// use the new renter-host protocol for hosts v1.4.0 or above
	if build.VersionCmp(params.Host.Version, "1.4.0") >= 0 {
		return cs.newFormContract(params, txnBuilder, tpool, hdb, cancel)
	}
	return cs.oldFormContract(params, txnBuilder, tpool, hdb, cancel)
}

func (cs *ContractSet) oldFormContract(params ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, err error) {
	// Extract vars from params, for convenience.
	allowance, host, funding, startHeight, endHeight, refundAddress := params.Allowance, params.Host, params.Funding, params.StartHeight, params.EndHeight, params.RefundAddress

	// Calculate the anticipated transaction fee.
	_, maxFee := tpool.FeeEstimation()
	txnFee := maxFee.Mul64(modules.EstimatedFileContractTransactionSetSize)

	period := endHeight - startHeight
	expectedStorage := allowance.ExpectedStorage / allowance.Hosts
	renterPayout, hostPayout, _, err := modules.RenterPayouts(host, funding, txnFee, types.ZeroCurrency, types.ZeroCurrency, period, expectedStorage)
	if err != nil {
		return modules.RenterContract{}, err
	}
	totalPayout := renterPayout.Add(hostPayout)

	// Check for negative currency.
	if totalPayout.Cmp(hostPayout) < 0 {
		return modules.RenterContract{}, errors.New("not enough money to pay both siafund fee and also host payout")
	}
	// Fund the transaction.
	_, err = txnBuilder.FundContract(funding)
	if err != nil {
		return modules.RenterContract{}, err
	}
	// Add FileContract identifier.
	fcTxn, _ := txnBuilder.View()
	si, hk := PrefixedSignedIdentifier(params.RenterSeed, fcTxn, host.PublicKey)
	_ = txnBuilder.AddArbitraryData(append(si[:], hk[:]...))
	// Create our key.
	ourSK, ourPK := GenerateKeyPair(params.RenterSeed, fcTxn)
	// Create unlock conditions.
	uc := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			types.Ed25519PublicKey(ourPK),
			host.PublicKey,
		},
		SignaturesRequired: 2,
	}

	// Create file contract.
	fc := types.FileContract{
		FileSize:       0,
		FileMerkleRoot: crypto.Hash{}, // no proof possible without data
		WindowStart:    endHeight,
		WindowEnd:      endHeight + host.WindowSize,
		Payout:         totalPayout,
		UnlockHash:     uc.UnlockHash(),
		RevisionNumber: 0,
		ValidProofOutputs: []types.SiacoinOutput{
			// This is the renter payout.
			{Value: totalPayout.Sub(hostPayout), UnlockHash: refundAddress},
			// Collateral is returned to host.
			{Value: hostPayout, UnlockHash: host.UnlockHash},
		},
		MissedProofOutputs: []types.SiacoinOutput{
			// Same as above.
			{Value: totalPayout.Sub(hostPayout), UnlockHash: refundAddress},
			// Same as above.
			{Value: hostPayout, UnlockHash: host.UnlockHash},
			// Once we start doing revisions, we'll move some coins to the host and some to the void.
			{Value: types.ZeroCurrency, UnlockHash: types.UnlockHash{}},
		},
	}

	// Build transaction containing fc, e.g. the File Contract.
	// Add file contract.
	txnBuilder.AddFileContract(fc)
	// Add miner fee.
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
		if err != nil {
			hdb.IncrementFailedInteractions(host.PublicKey)
			err = errors.Extend(err, modules.ErrHostFault)
		} else {
			hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Initiate connection.
	dialer := &net.Dialer{
		Cancel:  cancel,
		Timeout: connTimeout,
	}
	conn, err := dialer.Dial("tcp", string(host.NetAddress))
	if err != nil {
		return modules.RenterContract{}, err
	}
	defer func() { _ = conn.Close() }()

	// Allot time for sending RPC ID + verifySettings.
	extendDeadline(conn, modules.NegotiateSettingsTime)
	if err = encoding.WriteObject(conn, modules.RPCFormContract); err != nil {
		return modules.RenterContract{}, err
	}

	// Verify the host's settings and confirm its identity.
	host, err = verifySettings(conn, host)
	if err != nil {
		return modules.RenterContract{}, err
	}
	if !host.AcceptingContracts {
		return modules.RenterContract{}, errors.New("host is not accepting contracts")
	}

	// Allot time for negotiation.
	extendDeadline(conn, modules.NegotiateFileContractTime)

	// Send acceptance, txn signed by us, and pubkey.
	if err = modules.WriteNegotiationAcceptance(conn); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send initial acceptance: " + err.Error())
	}
	if err = encoding.WriteObject(conn, txnSet); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send the contract signed by us: " + err.Error())
	}
	if err = encoding.WriteObject(conn, ourSK.PublicKey()); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send our public key: " + err.Error())
	}

	// Read acceptance and txn signed by host.
	if err = modules.ReadNegotiationAcceptance(conn); err != nil {
		return modules.RenterContract{}, errors.New("host did not accept our proposed contract: " + err.Error())
	}
	// Host now sends any new parent transactions, inputs and outputs that
	// were added to the transaction.
	var newRefundOutputs []types.SiacoinOutput
	var newInputs []types.SiacoinInput
	if err = encoding.ReadObject(conn, &newRefundOutputs, types.BlockSizeLimit); err != nil {
		return modules.RenterContract{}, errors.New("couldn't read the host's added newRefundOutputs: " + err.Error())
	}
	if err = encoding.ReadObject(conn, &newInputs, types.BlockSizeLimit); err != nil {
		return modules.RenterContract{}, errors.New("couldn't read the host's added inputs: " + err.Error())
	}

	// Merge txnAdditions with txnSet.
	for _, output := range newRefundOutputs {
		txnBuilder.AddSiacoinOutput(output)
	}
	for _, input := range newInputs {
		txnBuilder.AddSiacoinInput(input)
	}

	// Sign the txn.
	signedTxnSet, err := txnBuilder.Sign(true)
	if err != nil {
		return modules.RenterContract{}, modules.WriteNegotiationRejection(conn, errors.New("failed to sign transaction: "+err.Error()))
	}

	// Calculate signatures added by the transaction builder.
	var addedSignatures []types.TransactionSignature
	_, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet)-1].TransactionSignatures[i])
	}

	// create initial (no-op) revision, transaction, and signature
	initRevision := types.FileContractRevision{
		ParentID:          signedTxnSet[len(signedTxnSet)-1].FileContractID(0),
		UnlockConditions:  uc,
		NewRevisionNumber: 1,

		NewFileSize:           fc.FileSize,
		NewFileMerkleRoot:     fc.FileMerkleRoot,
		NewWindowStart:        fc.WindowStart,
		NewWindowEnd:          fc.WindowEnd,
		NewValidProofOutputs:  fc.ValidProofOutputs,
		NewMissedProofOutputs: fc.MissedProofOutputs,
		NewUnlockHash:         fc.UnlockHash,
	}
	renterRevisionSig := types.TransactionSignature{
		ParentID:       crypto.Hash(initRevision.ParentID),
		PublicKeyIndex: 0,
		CoveredFields: types.CoveredFields{
			FileContractRevisions: []uint64{0},
		},
	}
	revisionTxn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{initRevision},
		TransactionSignatures: []types.TransactionSignature{renterRevisionSig},
	}
	encodedSig := crypto.SignHash(revisionTxn.SigHash(0), ourSK)
	revisionTxn.TransactionSignatures[0].Signature = encodedSig[:]

	// Send acceptance and signatures.
	if err = modules.WriteNegotiationAcceptance(conn); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send transaction acceptance: " + err.Error())
	}
	if err = encoding.WriteObject(conn, addedSignatures); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send added signatures: " + err.Error())
	}
	if err = encoding.WriteObject(conn, revisionTxn.TransactionSignatures[0]); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send revision signature: " + err.Error())
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
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Construct contract header.
	header := contractHeader{
		Transaction: revisionTxn,
		SecretKey:   ourSK,
		StartHeight: startHeight,
		TotalCost:   funding,
		ContractFee: host.ContractPrice,
		TxnFee:      txnFee,
		Utility: modules.ContractUtility{
			GoodForUpload: true,
			GoodForRenew:  true,
		},
	}

	// Add contract to set.
	meta, err := cs.managedInsertContract(header, nil) // no Merkle roots yet
	if err != nil {
		return modules.RenterContract{}, err
	}
	return meta, nil
}

// newFormContract forms a contract with a host using the new renter-host
// protocol.
func (cs *ContractSet) newFormContract(params ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, err error) {
	// Extract vars from params, for convenience.
	allowance, host, funding, startHeight, endHeight, refundAddress := params.Allowance, params.Host, params.Funding, params.StartHeight, params.EndHeight, params.RefundAddress

	// Calculate the anticipated transaction fee.
	_, maxFee := tpool.FeeEstimation()
	txnFee := maxFee.Mul64(modules.EstimatedFileContractTransactionSetSize)

	// Calculate the payouts for the renter, host, and whole contract.
	period := endHeight - startHeight
	expectedStorage := allowance.ExpectedStorage / allowance.Hosts
	renterPayout, hostPayout, _, err := modules.RenterPayouts(host, funding, txnFee, types.ZeroCurrency, types.ZeroCurrency, period, expectedStorage)
	if err != nil {
		return modules.RenterContract{}, err
	}
	totalPayout := renterPayout.Add(hostPayout)

	// Check for negative currency.
	if totalPayout.Cmp(hostPayout) < 0 {
		return modules.RenterContract{}, errors.New("not enough money to pay both siafund fee and also host payout")
	}
	// Fund the transaction.
	err = txnBuilder.FundSiacoins(funding)
	if err != nil {
		return modules.RenterContract{}, err
	}
	// Add FileContract identifier.
	fcTxn, _ := txnBuilder.View()
	si, hk := PrefixedSignedIdentifier(params.RenterSeed, fcTxn, host.PublicKey)
	_ = txnBuilder.AddArbitraryData(append(si[:], hk[:]...))
	// Create our key.
	ourSK, ourPK := GenerateKeyPair(params.RenterSeed, fcTxn)
	// Create unlock conditions.
	uc := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			types.Ed25519PublicKey(ourPK),
			host.PublicKey,
		},
		SignaturesRequired: 2,
	}

	// Create file contract.
	fc := types.FileContract{
		FileSize:       0,
		FileMerkleRoot: crypto.Hash{}, // no proof possible without data
		WindowStart:    endHeight,
		WindowEnd:      endHeight + host.WindowSize,
		Payout:         totalPayout,
		UnlockHash:     uc.UnlockHash(),
		RevisionNumber: 0,
		ValidProofOutputs: []types.SiacoinOutput{
			// Outputs need to account for tax.
			{Value: totalPayout.Sub(hostPayout), UnlockHash: refundAddress}, // This is the renter payout, but with tax applied.
			// Collateral is returned to host.
			{Value: hostPayout, UnlockHash: host.UnlockHash},
		},
		MissedProofOutputs: []types.SiacoinOutput{
			// Same as above.
			{Value: totalPayout.Sub(hostPayout), UnlockHash: refundAddress},
			// Same as above.
			{Value: hostPayout, UnlockHash: host.UnlockHash},
			// Once we start doing revisions, we'll move some coins to the host and some to the void.
			{Value: types.ZeroCurrency, UnlockHash: types.UnlockHash{}},
		},
	}

	// Add file contract.
	txnBuilder.AddFileContract(fc)
	// Add miner fee.
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
		if err != nil {
			hdb.IncrementFailedInteractions(host.PublicKey)
			err = errors.Extend(err, modules.ErrHostFault)
		} else {
			hdb.IncrementSuccessfulInteractions(host.PublicKey)
		}
	}()

	// Initiate protocol.
	s, err := cs.NewSessionWithSecret(host, types.FileContractID{}, startHeight, hdb, crypto.SecretKey{}, cancel)
	if err != nil {
		return modules.RenterContract{}, err
	}
	defer s.Close()

	// Send the FormContract request.
	req := modules.LoopFormContractRequest{
		Transactions: txnSet,
		RenterKey:    uc.PublicKeys[0],
	}
	if err := s.writeRequest(modules.RPCLoopFormContract, req); err != nil {
		return modules.RenterContract{}, err
	}

	// Read the host's response.
	var resp modules.LoopContractAdditions
	if err := s.readResponse(&resp); err != nil {
		return modules.RenterContract{}, err
	}

	// Incorporate host's modifications.
	txnBuilder.AddParents(resp.Parents)
	for _, input := range resp.Inputs {
		txnBuilder.AddSiacoinInput(input)
	}
	for _, output := range resp.Outputs {
		txnBuilder.AddSiacoinOutput(output)
	}

	// Sign the txn.
	signedTxnSet, err := txnBuilder.Sign(true)
	if err != nil {
		err = errors.New("failed to sign transaction: " + err.Error())
		modules.WriteRPCResponse(s.conn, s.aead, nil, err)
		return modules.RenterContract{}, err
	}

	// Calculate signatures added by the transaction builder.
	var addedSignatures []types.TransactionSignature
	_, _, addedSignatureIndices := txnBuilder.ViewAdded()
	for _, i := range addedSignatureIndices {
		addedSignatures = append(addedSignatures, signedTxnSet[len(signedTxnSet)-1].TransactionSignatures[i])
	}

	// create initial (no-op) revision, transaction, and signature
	initRevision := types.FileContractRevision{
		ParentID:          signedTxnSet[len(signedTxnSet)-1].FileContractID(0),
		UnlockConditions:  uc,
		NewRevisionNumber: 1,

		NewFileSize:           fc.FileSize,
		NewFileMerkleRoot:     fc.FileMerkleRoot,
		NewWindowStart:        fc.WindowStart,
		NewWindowEnd:          fc.WindowEnd,
		NewValidProofOutputs:  fc.ValidProofOutputs,
		NewMissedProofOutputs: fc.MissedProofOutputs,
		NewUnlockHash:         fc.UnlockHash,
	}
	renterRevisionSig := types.TransactionSignature{
		ParentID:       crypto.Hash(initRevision.ParentID),
		PublicKeyIndex: 0,
		CoveredFields: types.CoveredFields{
			FileContractRevisions: []uint64{0},
		},
	}
	revisionTxn := types.Transaction{
		FileContractRevisions: []types.FileContractRevision{initRevision},
		TransactionSignatures: []types.TransactionSignature{renterRevisionSig},
	}
	encodedSig := crypto.SignHash(revisionTxn.SigHash(0), ourSK)
	revisionTxn.TransactionSignatures[0].Signature = encodedSig[:]

	// Send acceptance and signatures.
	renterSigs := modules.LoopContractSignatures{
		ContractSignatures: addedSignatures,
		RevisionSignature:  revisionTxn.TransactionSignatures[0],
	}
	if err := modules.WriteRPCResponse(s.conn, s.aead, renterSigs, nil); err != nil {
		return modules.RenterContract{}, err
	}

	// Read the host acceptance and signatures.
	var hostSigs modules.LoopContractSignatures
	if err := s.readResponse(&hostSigs); err != nil {
		return modules.RenterContract{}, err
	}
	for _, sig := range hostSigs.ContractSignatures {
		txnBuilder.AddTransactionSignature(sig)
	}
	revisionTxn.TransactionSignatures = append(revisionTxn.TransactionSignatures, hostSigs.RevisionSignature)

	// Construct the final transaction.
	txn, parentTxns = txnBuilder.View()
	txnSet = append(parentTxns, txn)

	// Submit to blockchain.
	err = tpool.AcceptTransactionSet(txnSet)
	if err == modules.ErrDuplicateTransactionSet {
		// As long as it made it into the transaction pool, we're good.
		err = nil
	}
	if err != nil {
		return modules.RenterContract{}, err
	}

	// Construct contract header.
	header := contractHeader{
		Transaction: revisionTxn,
		SecretKey:   ourSK,
		StartHeight: startHeight,
		TotalCost:   funding,
		ContractFee: host.ContractPrice,
		TxnFee:      txnFee,
		Utility: modules.ContractUtility{
			GoodForUpload: true,
			GoodForRenew:  true,
		},
	}

	// Add contract to set.
	meta, err := cs.managedInsertContract(header, nil) // no Merkle roots yet
	if err != nil {
		return modules.RenterContract{}, err
	}
	return meta, nil
}
