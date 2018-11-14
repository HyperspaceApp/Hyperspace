package wallet

import (
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

//TODO: Document it

type transactionSetBuilder struct {
	signed   bool
	outputs  []types.SiacoinOutput

	builders []transactionBuilder

	wallet *Wallet
}

func (tb *transactionSetBuilder) currentBuilder() *transactionBuilder {
	return &tb.builders[len(tb.builders)-1]
}

func (w *Wallet) StartTransactionSet() (modules.TransactionSetBuilder, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()
	return w.RegisterTransactionSet(types.Transaction{}, nil)
}

func (w *Wallet) RegisterTransactionSet(t types.Transaction, parents []types.Transaction) (modules.TransactionSetBuilder, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.registerTransactionSet(t, parents), nil
}

func (w *Wallet) registerTransactionSet(t types.Transaction, parents []types.Transaction) (*transactionSetBuilder) {
	ret := transactionSetBuilder {
		wallet: w,
	}

	// Add the default builder
	builder := w.registerTransaction(t, parents)
	ret.builders = append(ret.builders, *builder)

	return &ret
}

// Wallet need to be locked before calling this
func (tb *transactionSetBuilder) checkDefragCondition(dustThreshold types.Currency) (bool, error) {
	consensusHeight, err := dbGetConsensusHeight(tb.wallet.dbTx)
	if err != nil {
		return false, err
	}

	// Collect a value-sorted set of siacoin outputs.
	var so sortedOutputs
	err = dbForEachSiacoinOutput(tb.wallet.dbTx, func(scoid types.SiacoinOutputID, sco types.SiacoinOutput) {
		if tb.wallet.checkOutput(tb.wallet.dbTx, consensusHeight, scoid, sco, dustThreshold) == nil {
			so.ids = append(so.ids, scoid)
			so.outputs = append(so.outputs, sco)
		}
	})
	if err != nil {
		return false, err
	}

	// Only defrag if there are enough outputs to merit defragging.
	if len(so.ids) <= defragThreshold {
		return false, nil
	}
	return true, nil
}

func (tb *transactionSetBuilder) FundOutput(output types.SiacoinOutput, fee types.Currency) error {
	var outputs []types.SiacoinOutput
	return tb.FundOutputs(append(outputs, output), fee)
}

func (tb *transactionSetBuilder) FundOutputs(outputs []types.SiacoinOutput, fee types.Currency) error {
	// dustThreshold has to be obtained separate from the lock
	dustThreshold, err := tb.wallet.DustThreshold()
	if err != nil {
		return err
	}

	consensusHeight, err := dbGetConsensusHeight(tb.wallet.dbTx)
	if err != nil {
		return err
	}

	tb.wallet.mu.Lock()
	defer tb.wallet.mu.Unlock()

	// Check how many spendable outputs we have, if there aren't enough
	// to justify a defrag just fallback to the first (default) builder.
	ret, err := tb.checkDefragCondition(dustThreshold)
	if (err != nil) {
		return err
	} else if (ret == false) {
		return tb.currentBuilder().FundOutputs(outputs, fee)
	}

	// If that's not the case, then we will try to fill a txnset up
	// to the valid transaction set size.
	var totalFund types.Currency
	var refund types.SiacoinOutput
	amount := calculateAmountFromOutputs(outputs, fee)
	addFee := true
	needRefund := true
	txFilled := false

	for i := range outputs {
		needRefund = true
		builder := tb.currentBuilder()

		if (txFilled) {
			// Add a new fresh builder for the next outputs
			newBuilder := tb.wallet.registerTransaction(types.Transaction{}, nil)
			if (err != nil) {
				return err
			}
			tb.builders = append(tb.builders, *newBuilder)
			txFilled = false
		}

		if (addFee) {
			builder.AddMinerFee(fee)
			addFee = false;
		}

		addedFunds, scoids, err := builder.fundOutput(outputs[i], refund)
		if (err != nil) {
			return err
		}

		totalFund = totalFund.Add(addedFunds)

		tx, _ := builder.View()

		if (tx.MarshalSiaSize() >= modules.TransactionSizeLimit - 2e3) {
			// TODO: this refund should be used by the next builder
			// it should be possible to just pass it to the next call
			refund, err = builder.checkRefund(amount, totalFund)
			if (err != nil) {
				return err
			}
			needRefund = false
			// Mark all outputs that were spent as spent
			for _, scoid := range scoids {
				err := dbPutSpentOutput(tb.wallet.dbTx, types.OutputID(scoid), consensusHeight)
				if err != nil {
					return err
				}
			}
			// We will add a fee to each builder
			addFee = true
			txFilled = true
		}
	}

	// Check if the last transaction need a refund
	if (needRefund == true) {
		_, err := tb.currentBuilder().checkRefund(amount, totalFund)
		if (err != nil) {
			return err
		}
	}

	// TODO: check size
	return nil
}

func (tb *transactionSetBuilder) AddOutput(output types.SiacoinOutput) uint64 {
	return tb.currentBuilder().AddSiacoinOutput(output)
}

func (tb *transactionSetBuilder) AddInput(output types.SiacoinInput) uint64 {
	return tb.currentBuilder().AddSiacoinInput(output)
}

func (tb *transactionSetBuilder) Sign(wholeTransaction bool) ([]types.Transaction, error) {
	if tb.signed {
		return nil, errBuilderAlreadySigned
	}

	tb.wallet.mu.RLock()
	defer tb.wallet.mu.RUnlock()

	// Sign the first builder
	txSet, err := tb.builders[0].Sign(wholeTransaction)
	if (err != nil) {
		return nil, err
	}

	// Let's see if there are more tx to be added in this set
	// otherwise return just a set with one transaction.
	for i := 1; i < len(tb.builders); i++ {
		tb.builders[i].AddParents(txSet)
		tx, err := tb.builders[i].Sign(wholeTransaction)
		if (err != nil) {
			return nil, err
		}
		tb.signed = true
		txSet = tx
	}

	return txSet, nil
}

func (tb *transactionSetBuilder) View() (types.Transaction, []types.Transaction) {
	// The last builder, is the actual transaction
	// other builders will be returned as parents.
	var ret []types.Transaction
	for i := range tb.builders {
		tx, _ := tb.builders[i].View()
		ret = append(ret, tx) 
	}
	return ret[len(ret)-1], ret[:len(ret)-1]
}

func (tb *transactionSetBuilder) Size() (size int) {
	var ret int
	for i := range tb.builders {
		tx, _ := tb.builders[i].View()
		ret += tx.MarshalSiaSize()
	}
	return ret
}
