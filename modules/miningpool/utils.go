package pool

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"runtime"

	"github.com/NebulousLabs/Sia/types"
)

func interfaceify(strs []string) []interface{} {
	newslice := make([]interface{}, len(strs))
	for i, v := range strs {
		newslice[i] = v
	}
	return newslice
}

func trace() {
    pc := make([]uintptr, 10)  // at least 1 entry needed
    runtime.Callers(2, pc)
    f := runtime.FuncForPC(pc[0])
    file, line := f.FileLine(pc[0])
    fmt.Printf("%s:%d %s\n", file, line, f.Name())
}

func sPrintID(id uint64) string {
	return fmt.Sprintf("%016x", id)
}

func ssPrintID(id int64) string {
	return fmt.Sprintf("%d", id)
}

func sPrintTx(txn types.Transaction) string {
	var buffer bytes.Buffer
	// TODO print unlock conditions
	for i, input := range txn.SiacoinInputs {
		buffer.WriteString(fmt.Sprintf("Siacoin Input: %d, Parent ID: %s\n", i, input.ParentID))
	}
	for i, output := range txn.SiacoinOutputs {
		buffer.WriteString(fmt.Sprintf("Siacoin Output: %d, ID: %s,\n Value: %s, Unlock Hash: %s\n",
			i, txn.SiacoinOutputID(uint64(i)).String(), output.Value.String(), output.UnlockHash))
	}
	// TODO FileContracts, FileContractRevisions, StorageProofs, SiafundInputs, SiafundOutputs
	for i, fee := range txn.MinerFees {
		buffer.WriteString(fmt.Sprintf("Miner Fee: %d, Value: %s\n", i, fee.String()))
	}
	for i, datum := range txn.ArbitraryData {
		buffer.WriteString(fmt.Sprintf("Arbitrary Datum: %d, Value: %s\n", i, datum))
	}
	// TODO rest of transaction
	return buffer.String()
}

// TODO
func sPrintBlock(b types.Block) string {
	var buffer bytes.Buffer
	buffer.WriteString(fmt.Sprintf("Block ID: %s\n, Parent ID: %d\nNonce: %d\nTimestamp: %d\n", b.ID(), b.ParentID, b.Nonce, b.Timestamp))
	buffer.WriteString("\tMiner Payouts:\n")
	for i, txn := range b.MinerPayouts {
		buffer.WriteString(fmt.Sprintf("\t\tPayout %d: %s\n", i, txn))
	}
	buffer.WriteString("\tTransactions:\n")
	for i, txn := range b.Transactions {
		buffer.WriteString(fmt.Sprintf("\t\tTx %d:\n", i))
		buffer.WriteString(sPrintTx(txn))
		buffer.WriteString("\n")
	}
	buffer.WriteString("\n")
	return buffer.String()
}

func printWithSuffix(number types.Currency) string {
	num := number.Big().Uint64()

	if num > 1000000000000000 {
		return fmt.Sprintf("%dP", num/1000000000000000)
	}
	if num > 1000000000000 {
		return fmt.Sprintf("%dT", num/1000000000000)
	}
	if num > 1000000000 {
		return fmt.Sprintf("%dG", num/1000000000)
	}
	if num > 1000000 {
		return fmt.Sprintf("%dM", num/1000000)
	}
	return fmt.Sprintf("%d", num)
}

// IntToTarget converts a big.Int to a Target.
func intToTarget(i *big.Int) (t types.Target, err error) {
	// Check for negatives.
	if i.Sign() < 0 {
		err = errors.New("Negative target")
		return
	}
	// In the event of overflow, return the maximum.
	if i.BitLen() > 256 {
		err = errors.New("Target is too high")
		return
	}
	b := i.Bytes()
	offset := len(t[:]) - len(b)
	copy(t[offset:], b)
	return
}

func difficultyToTarget(difficulty float64) (target types.Target, err error) {
	diffAsBig := big.NewFloat(difficulty)

	diffOneString := "0x00000000ffff0000000000000000000000000000000000000000000000000000"
	targetOneAsBigInt := &big.Int{}
	targetOneAsBigInt.SetString(diffOneString, 0)

	targetAsBigFloat := &big.Float{}
	targetAsBigFloat.SetInt(targetOneAsBigInt)
	targetAsBigFloat.Quo(targetAsBigFloat, diffAsBig)
	targetAsBigInt, _ := targetAsBigFloat.Int(nil)
	target, err = intToTarget(targetAsBigInt)
	return
}

//
// The following function is covered by the included license
// The function was copied from https://github.com/btcsuite/btcd
//
// ISC License

// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers

// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.

// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

// BigToCompact converts a whole number N to a compact representation using
// an unsigned 32-bit number.  The compact representation only provides 23 bits
// of precision, so values larger than (2^23 - 1) only encode the most
// significant digits of the number.  See CompactToBig for details.
func BigToCompact(n *big.Int) uint32 {
	// No need to do any work if it's zero.
	if n.Sign() == 0 {
		return 0
	}

	// Since the base for the exponent is 256, the exponent can be treated
	// as the number of bytes.  So, shift the number right or left
	// accordingly.  This is equivalent to:
	// mantissa = mantissa / 256^(exponent-3)
	var mantissa uint32
	exponent := uint(len(n.Bytes()))
	if exponent <= 3 {
		mantissa = uint32(n.Bits()[0])
		mantissa <<= 8 * (3 - exponent)
	} else {
		// Use a copy to avoid modifying the caller's original number.
		tn := new(big.Int).Set(n)
		mantissa = uint32(tn.Rsh(tn, 8*(exponent-3)).Bits()[0])
	}

	// When the mantissa already has the sign bit set, the number is too
	// large to fit into the available 23-bits, so divide the number by 256
	// and increment the exponent accordingly.
	if mantissa&0x00800000 != 0 {
		mantissa >>= 8
		exponent++
	}

	// Pack the exponent, sign bit, and mantissa into an unsigned 32-bit
	// int and return it.
	compact := uint32(exponent<<24) | mantissa
	if n.Sign() < 0 {
		compact |= 0x00800000
	}
	return compact
}
