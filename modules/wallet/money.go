package wallet

import (
	"errors"
	"sort"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

// sortedOutputs is a struct containing a slice of siacoin outputs and their
// corresponding ids. sortedOutputs can be sorted using the sort package.
type sortedOutputs struct {
	ids     []types.SiacoinOutputID
	outputs []types.SiacoinOutput
}

// DustThreshold returns the quantity per byte below which a Currency is
// considered to be Dust.
func (w *Wallet) DustThreshold() (types.Currency, error) {
	if err := w.tg.Add(); err != nil {
		return types.Currency{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	minFee, _ := w.tpool.FeeEstimation()
	return minFee.Mul64(3), nil
}

// ConfirmedBalance returns the balance of the wallet according to all of the
// confirmed transactions.
func (w *Wallet) ConfirmedBalance() (siacoinBalance types.Currency, err error) {
	if err := w.tg.Add(); err != nil {
		return types.ZeroCurrency, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	// dustThreshold has to be obtained separate from the lock
	dustThreshold, err := w.DustThreshold()
	if err != nil {
		return types.ZeroCurrency, modules.ErrWalletShutdown
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// ensure durability of reported balance
	if err = w.syncDB(); err != nil {
		return
	}

	dbForEachSiacoinOutput(w.dbTx, func(_ types.SiacoinOutputID, sco types.SiacoinOutput) {
		if sco.Value.Cmp(dustThreshold) > 0 {
			siacoinBalance = siacoinBalance.Add(sco.Value)
		}
	})

	return
}

// UnconfirmedBalance returns the number of outgoing and incoming siacoins in
// the unconfirmed transaction set. Refund outputs are included in this
// reporting.
func (w *Wallet) UnconfirmedBalance() (outgoingSiacoins types.Currency, incomingSiacoins types.Currency, err error) {
	if err := w.tg.Add(); err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	// dustThreshold has to be obtained separate from the lock
	dustThreshold, err := w.DustThreshold()
	if err != nil {
		return types.ZeroCurrency, types.ZeroCurrency, modules.ErrWalletShutdown
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, upt := range w.unconfirmedProcessedTransactions {
		for _, input := range upt.Inputs {
			if input.FundType == types.SpecifierSiacoinInput && input.WalletAddress {
				outgoingSiacoins = outgoingSiacoins.Add(input.Value)
			}
		}
		for _, output := range upt.Outputs {
			if output.FundType == types.SpecifierSiacoinOutput && output.WalletAddress && output.Value.Cmp(dustThreshold) > 0 {
				incomingSiacoins = incomingSiacoins.Add(output.Value)
			}
		}
	}
	return
}

// getSortedOutputs is a helper function that returns outputs sorted from most recent
// to oldest
func (w *Wallet) getSortedOutputs() (so sortedOutputs, err error) {
	// Collect a value-sorted set of siacoin outputs.
	err = dbForEachSiacoinOutput(w.dbTx, func(scoid types.SiacoinOutputID, sco types.SiacoinOutput) {
		so.ids = append(so.ids, scoid)
		so.outputs = append(so.outputs, sco)
	})
	if err != nil {
		return so, err
	}
	// Add all of the unconfirmed outputs as well.
	for _, upt := range w.unconfirmedProcessedTransactions {
		for i, sco := range upt.Transaction.SiacoinOutputs {
			// Determine if the output belongs to the wallet.
			_, exists := w.keys[sco.UnlockHash]
			if !exists {
				continue
			}
			so.ids = append(so.ids, upt.Transaction.SiacoinOutputID(uint64(i)))
			so.outputs = append(so.outputs, sco)
		}
	}
	sort.Sort(sort.Reverse(so))
	return
}

// checkOutput is a helper function used to determine if an output is usable.
// TODO this doesn't thrown an error on old spent outputs, but it should
func (w *Wallet) checkOutput(tx *bolt.Tx, currentHeight types.BlockHeight, id types.SiacoinOutputID, output types.SiacoinOutput, dustThreshold types.Currency) error {
	// Check that an output is not dust
	if output.Value.Cmp(dustThreshold) < 0 {
		return errDustOutput
	}
	// Check that this output has not recently been spent by the wallet.
	spendHeight, err := dbGetSpentOutput(tx, types.OutputID(id))
	if err == nil {
		if spendHeight+RespendTimeout > currentHeight {
			return errSpendHeightTooHigh
		}
	}
	outputUnlockConditions := w.keys[output.UnlockHash].UnlockConditions
	if currentHeight < outputUnlockConditions.Timelock {
		return errOutputTimelock
	}

	return nil
}

// UnspentOutputs returns all unspent outputs relative to the wallet
// TODO this currently relies on the broken checkOutput function
func (w *Wallet) UnspentOutputs() (outputs []types.SiacoinOutput, err error) {
	// dustThreshold has to be obtained separate from the lock
	dustThreshold, err := w.DustThreshold()
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	height, err := dbGetConsensusHeight(w.dbTx)
	if err != nil {
		return nil, err
	}

	so, err := w.getSortedOutputs()
	if err != nil {
		return nil, err
	}
	for i := range so.outputs {
		scoid := so.ids[i]
		sco := so.outputs[i]
		if spendableErr := w.checkOutput(w.dbTx, height, scoid, sco, dustThreshold); spendableErr == nil {
			outputs = append(outputs, sco)
		}
	}
	return
}

// SendSiacoins creates a transaction sending 'amount' to 'dest'. The transaction
// is submitted to the transaction pool and is also returned.
func (w *Wallet) SendSiacoins(amount types.Currency, dest types.UnlockHash) (txns []types.Transaction, err error) {
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()

	w.mu.RLock()
	unlocked := w.unlocked
	w.mu.RUnlock()
	if !unlocked {
		w.log.Println("Attempt to send coins has failed - wallet is locked")
		return nil, modules.ErrLockedWallet
	}

	_, tpoolFee := w.tpool.FeeEstimation()
	tpoolFee = tpoolFee.Mul64(750) // Estimated transaction size in bytes
	output := types.SiacoinOutput{
		Value:      amount,
		UnlockHash: dest,
	}

	txnBuilder, err := w.StartTransaction()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			txnBuilder.Drop()
		}
	}()
	err = txnBuilder.FundSiacoinsForOutputs([]types.SiacoinOutput{output}, tpoolFee)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - failed to fund transaction:", err)
		return nil, build.ExtendErr("unable to fund transaction", err)
	}
	txnSet, err := txnBuilder.Sign(true)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - failed to sign transaction:", err)
		return nil, build.ExtendErr("unable to sign transaction", err)
	}
	if w.deps.Disrupt("SendSiacoinsInterrupted") {
		return nil, errors.New("failed to accept transaction set (SendSiacoinsInterrupted)")
	}
	err = w.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - transaction pool rejected transaction:", err)
		return nil, build.ExtendErr("unable to get transaction accepted", err)
	}
	w.log.Println("Submitted a siacoin transfer transaction set for value", amount.HumanString(), "with fees", tpoolFee.HumanString(), "IDs:")
	for _, txn := range txnSet {
		w.log.Println("\t", txn.ID())
	}
	return txnSet, nil
}

// SendSiacoinsMulti creates a transaction that includes the specified
// outputs. The transaction is submitted to the transaction pool and is also
// returned.
func (w *Wallet) SendSiacoinsMulti(outputs []types.SiacoinOutput) (txns []types.Transaction, err error) {
	w.log.Println("Beginning call to SendSiacoinsMulti")
	if err := w.tg.Add(); err != nil {
		err = modules.ErrWalletShutdown
		return nil, err
	}
	defer w.tg.Done()
	w.mu.RLock()
	unlocked := w.unlocked
	w.mu.RUnlock()
	if !unlocked {
		w.log.Println("Attempt to send coins has failed - wallet is locked")
		return nil, modules.ErrLockedWallet
	}

	txnBuilder, err := w.StartTransaction()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			txnBuilder.Drop()
		}
	}()

	// Add estimated transaction fee.
	_, tpoolFee := w.tpool.FeeEstimation()
	tpoolFee = tpoolFee.Mul64(2)                              // We don't want send-to-many transactions to fail.
	tpoolFee = tpoolFee.Mul64(1000 + 60*uint64(len(outputs))) // Estimated transaction size in bytes

	err = txnBuilder.FundSiacoinsForOutputs(outputs, tpoolFee)
	if err != nil {
		return nil, build.ExtendErr("unable to fund transaction", err)
	}

	txnSet, err := txnBuilder.Sign(true)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - failed to sign transaction:", err)
		return nil, build.ExtendErr("unable to sign transaction", err)
	}
	if w.deps.Disrupt("SendSiacoinsInterrupted") {
		return nil, errors.New("failed to accept transaction set (SendSiacoinsInterrupted)")
	}
	w.log.Println("Attempting to broadcast a multi-send over the network")
	err = w.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		w.log.Println("Attempt to send coins has failed - transaction pool rejected transaction:", err)
		return nil, build.ExtendErr("unable to get transaction accepted", err)
	}

	// Log the success.
	var outputList string
	for _, output := range outputs {
		outputList = outputList + "\n\tAddress: " + output.UnlockHash.String() + "\n\tValue: " + output.Value.HumanString() + "\n"
	}
	w.log.Printf("Successfully broadcast transaction with id %v, fee %v, and the following outputs: %v", txnSet[len(txnSet)-1].ID(), tpoolFee.HumanString(), outputList)
	return txnSet, nil
}

// Len returns the number of elements in the sortedOutputs struct.
func (so sortedOutputs) Len() int {
	if build.DEBUG && len(so.ids) != len(so.outputs) {
		panic("sortedOutputs object is corrupt")
	}
	return len(so.ids)
}

// Less returns whether element 'i' is less than element 'j'. The currency
// value of each output is used for comparison.
func (so sortedOutputs) Less(i, j int) bool {
	return so.outputs[i].Value.Cmp(so.outputs[j].Value) < 0
}

// Swap swaps two elements in the sortedOutputs set.
func (so sortedOutputs) Swap(i, j int) {
	so.ids[i], so.ids[j] = so.ids[j], so.ids[i]
	so.outputs[i], so.outputs[j] = so.outputs[j], so.outputs[i]
}
