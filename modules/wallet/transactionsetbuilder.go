package wallet

import (
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

//TODO: Document it

type transactionSetBuilder struct {
	signed   bool

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

func (tb *transactionSetBuilder) FundOutput(output types.SiacoinOutput, fee types.Currency) error {
	var outputs []types.SiacoinOutput
	return tb.FundOutputs(append(outputs, output), fee)
}

func (tb *transactionSetBuilder) FundOutputs(outputs []types.SiacoinOutput, fee types.Currency) error {
	consensusHeight, err := dbGetConsensusHeight(tb.wallet.dbTx)
	if err != nil {
		return err
	}

	tb.wallet.mu.Lock()
	defer tb.wallet.mu.Unlock()

	var scoids []types.SiacoinOutputID

	amount := fee
	totalFund := types.NewCurrency64(0)
	rest := types.NewCurrency64(0)
	addedFunds := types.NewCurrency64(0)

	// We need this to avoid the side case of
	// adding two refunds at the end of a new
	// txset.
	needRefund := true

	// Gather outputs
	so, err := tb.wallet.getSortedOutputs()
	if err != nil {
		return err
	}

	tb.currentBuilder().AddMinerFee(fee)

	for i := range outputs {
		needRefund = true
		tx, _ := tb.currentBuilder().View()
		if (tx.MarshalSiaSize() >= modules.TransactionSizeLimit - 2e3) {
			refund, err := tb.currentBuilder().checkRefund(amount, totalFund)
			if (err != nil) {
				return err
			}

			// Prepend the refund to the outputs so that the next builder
			// can use it.
			refundID := tx.SiacoinOutputID(uint64(len(tx.SiacoinOutputs))-1)
			so.ids = append([]types.SiacoinOutputID{refundID}, so.ids...)
			so.outputs = append([]types.SiacoinOutput{refund}, so.outputs...)

			// Spend the refund output so that it can be coherent
			// with consensus rules.
			err = dbPutSpentOutput(tb.wallet.dbTx, types.OutputID(refundID),
				consensusHeight)
			if err != nil {
				return err
			}

			amount = fee
			rest = types.NewCurrency64(0)
			totalFund = types.NewCurrency64(0)
			// Add a new fresh builder for the next outputs
			newBuilder := tb.wallet.registerTransaction(types.Transaction{}, nil)
			tb.builders = append(tb.builders, *newBuilder)
			tb.currentBuilder().AddMinerFee(fee)
			needRefund = false
		}

		// We already have enough funds?
		// Do not seek for inputs, just add the output.
		if (rest.Cmp(outputs[i].Value) >= 0) {
			// NOTE: The amount has been already added
			// to totalFund in the previous fundOutput call!
			rest = rest.Sub(outputs[i].Value)
			tb.currentBuilder().AddSiacoinOutput(outputs[i])
		} else {
			addedFunds, scoids, err = tb.currentBuilder().fundOutput(outputs[i], so)
			if (err != nil) {
				return err
			}
			rest = addedFunds.Sub(outputs[i].Value)
			totalFund = totalFund.Add(addedFunds)
		}
		amount = amount.Add(outputs[i].Value)

		// Mark all outputs that were spent as spent
		for _, scoid := range scoids {
			err := dbPutSpentOutput(tb.wallet.dbTx, types.OutputID(scoid), consensusHeight)
			if err != nil {
				return err
			}
		}
	}

	// Check if the last transaction need a refund
	if (needRefund) {
		_, err = tb.currentBuilder().checkRefund(amount, totalFund)
		if (err != nil) {
			return err
		}
	}
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
		ret += len(encoding.Marshal(tx))
	}
	return ret
}
