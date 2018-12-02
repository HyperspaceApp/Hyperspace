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

// FormContract forms a contract with a host and submits the contract
// transaction to tpool. The contract is added to the ContractSet and its
// metadata is returned.
// TODO joint signature
func (cs *ContractSet) FormContract(params ContractParams, txnBuilder transactionBuilder, tpool transactionPool, hdb hostDB, cancel <-chan struct{}) (rc modules.RenterContract, err error) {
	// Extract vars from params, for convenience.
	allowance, host, funding, startHeight, endHeight, refundAddress := params.Allowance, params.Host, params.Funding, params.StartHeight, params.EndHeight, params.RefundAddress

	// Create our keys.
	ourSK, ourPK := crypto.GenerateKeyPair()
	var renterPK, hostPK ed25519.PublicKey
	var privateKey ed25519.PrivateKey
	copy(hostPK[:], host.PublicKey.Key[:])
	copy(renterPK[:], ourPK[:])
	copy(privateKey[:], ourSK[:])
	publicKeys := []ed25519.PublicKey{hostPK, renterPK}
	jointPrivateKey, err := ed25519.GenerateJointPrivateKey(publicKeys, privateKey, 1)
	var jointPK crypto.PublicKey
	copy(jointPK[:], jointPrivateKey[32:])
	var ourJointSK crypto.SecretKey
	copy(ourJointSK[:], jointPrivateKey[:])

	// Create unlock conditions.
	/*
	uc := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			types.Ed25519PublicKey(ourPK),
			host.PublicKey,
		},
	}
	*/
	uc := types.UnlockConditions{
		PublicKeys: []types.SiaPublicKey{
			types.Ed25519PublicKey(jointPK),
		},
	}

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
	_, err = txnBuilder.FundContract(funding)
	if err != nil {
		return modules.RenterContract{}, err
	}
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

	// Send public key so the host can form their joint key
	extendDeadline(conn, modules.NegotiatePublicKeyTime)
	err = modules.WritePublicKey(conn, renterPK)
	if err != nil {
		return modules.RenterContract{}, err
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
	fcid := signedTxnSet[len(signedTxnSet)-1].FileContractID(0)
	revisionTxn := modules.BuildInitialRevisionTransaction(fc, fcid, jointPK)
	msg := revisionTxn.SigHash(0)
	renterNoncePoint := ed25519.GenerateNoncePoint(privateKey, msg[:])

	// Allot time for negotiation.
	extendDeadline(conn, modules.NegotiateCurvePointTime)
	// Send our contract revision tx nonce point to the host
	if err = modules.WriteCurvePoint(conn, renterNoncePoint); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send our contract transaction nonce point: " + err.Error())
	}
	var hostNoncePoint ed25519.CurvePoint
	if err = modules.ReadCurvePoint(conn, &hostNoncePoint); err != nil {
		return modules.RenterContract{}, errors.New("couldn't read the host's contract transaction nonce point: " + err.Error())
	}
	noncePoints := []ed25519.CurvePoint{hostNoncePoint, renterNoncePoint}
	renterRevisionSignature := ed25519.JointSign(privateKey, jointPrivateKey, noncePoints, msg[:])

	// Send acceptance and signatures.
	if err = modules.WriteNegotiationAcceptance(conn); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send transaction acceptance: " + err.Error())
	}
	if err = encoding.WriteObject(conn, addedSignatures); err != nil {
		return modules.RenterContract{}, errors.New("couldn't send added signatures: " + err.Error())
	}
	if err = modules.WriteSignature(conn, renterRevisionSignature); err != nil {
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
		Transaction:    revisionTxn,
		SecretKey:      ourSK,
		JointSecretKey: ourJointSK,
		StartHeight:    startHeight,
		TotalCost:      funding,
		ContractFee:    host.ContractPrice,
		TxnFee:         txnFee,
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
