package types

import (
	"bytes"
	"io"
	"unsafe"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
)

// MarshalSia implements the encoding.SiaMarshaler interface.
func (b BlockV0) MarshalSia(w io.Writer) error {
	if build.DEBUG {
		// Sanity check: compare against the old encoding
		buf := new(bytes.Buffer)
		encoding.NewEncoder(buf).EncodeAll(
			b.ParentID,
			b.Nonce,
			b.Timestamp,
			b.MinerPayouts,
			b.Transactions,
		)
		w = sanityCheckWriter{w, buf}
	}

	e := encoding.NewEncoder(w)
	e.Write(b.ParentID[:])
	e.Write(b.Nonce[:])
	e.WriteUint64(uint64(b.Timestamp))
	e.WriteInt(len(b.MinerPayouts))
	for i := range b.MinerPayouts {
		b.MinerPayouts[i].MarshalSia(e)
	}
	e.WriteInt(len(b.Transactions))
	for i := range b.Transactions {
		if err := b.Transactions[i].MarshalSia(e); err != nil {
			return err
		}
	}
	return e.Err()
}

// UnmarshalSia implements the encoding.SiaUnmarshaler interface.
func (b *BlockV0) UnmarshalSia(r io.Reader) error {
	if build.DEBUG {
		// Sanity check: compare against the old decoding
		buf := new(bytes.Buffer)
		r = io.TeeReader(r, buf)

		defer func() {
			checkB := new(Block)
			if err := encoding.UnmarshalAll(buf.Bytes(),
				&checkB.ParentID,
				&checkB.Nonce,
				&checkB.Timestamp,
				&checkB.MinerPayouts,
				&checkB.Transactions,
			); err != nil {
				// don't check invalid blocks
				return
			}
			if crypto.HashObject(b) != crypto.HashObject(checkB) {
				panic("decoding differs!")
			}
		}()
	}

	d := encoding.NewDecoder(r)
	d.ReadFull(b.ParentID[:])
	d.ReadFull(b.Nonce[:])
	b.Timestamp = Timestamp(d.NextUint64())
	// MinerPayouts
	b.MinerPayouts = make([]SiacoinOutput, d.NextPrefix(unsafe.Sizeof(SiacoinOutput{})))
	for i := range b.MinerPayouts {
		b.MinerPayouts[i].UnmarshalSia(d)
	}
	// Transactions
	b.Transactions = make([]TransactionV0, d.NextPrefix(unsafe.Sizeof(TransactionV0{})))
	for i := range b.Transactions {
		b.Transactions[i].UnmarshalSia(d)
	}
	return d.Err()
}

// MarshalSia implements the encoding.SiaMarshaler interface.
func (fcr FileContractRevisionV0) MarshalSia(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.Write(fcr.ParentID[:])
	fcr.UnlockConditions.MarshalSia(e)
	e.WriteUint64(fcr.NewRevisionNumber)
	e.WriteUint64(fcr.NewFileSize)
	e.Write(fcr.NewFileMerkleRoot[:])
	e.WriteUint64(uint64(fcr.NewWindowStart))
	e.WriteUint64(uint64(fcr.NewWindowEnd))
	e.WriteInt(len(fcr.NewValidProofOutputs))
	for _, sco := range fcr.NewValidProofOutputs {
		sco.MarshalSia(e)
	}
	e.WriteInt(len(fcr.NewMissedProofOutputs))
	for _, sco := range fcr.NewMissedProofOutputs {
		sco.MarshalSia(e)
	}
	e.Write(fcr.NewUnlockHash[:])
	return e.Err()
}

// MarshalSiaSize returns the encoded size of fcr.
func (fcr FileContractRevisionV0) MarshalSiaSize() (size int) {
	size += len(fcr.ParentID)
	size += fcr.UnlockConditions.MarshalSiaSize()
	size += 8 // NewRevisionNumber
	size += 8 // NewFileSize
	size += len(fcr.NewFileMerkleRoot)
	size += 8 + 8 // NewWindowStart + NewWindowEnd
	size += 8
	for _, sco := range fcr.NewValidProofOutputs {
		size += sco.Value.MarshalSiaSize()
		size += len(sco.UnlockHash)
	}
	size += 8
	for _, sco := range fcr.NewMissedProofOutputs {
		size += sco.Value.MarshalSiaSize()
		size += len(sco.UnlockHash)
	}
	size += len(fcr.NewUnlockHash)
	return
}

// UnmarshalSia implements the encoding.SiaUnmarshaler interface.
func (fcr *FileContractRevisionV0) UnmarshalSia(r io.Reader) error {
	d := encoding.NewDecoder(r)
	d.ReadFull(fcr.ParentID[:])
	fcr.UnlockConditions.UnmarshalSia(d)
	fcr.NewRevisionNumber = d.NextUint64()
	fcr.NewFileSize = d.NextUint64()
	d.ReadFull(fcr.NewFileMerkleRoot[:])
	fcr.NewWindowStart = BlockHeight(d.NextUint64())
	fcr.NewWindowEnd = BlockHeight(d.NextUint64())
	fcr.NewValidProofOutputs = make([]SiacoinOutput, d.NextPrefix(unsafe.Sizeof(SiacoinOutput{})))
	for i := range fcr.NewValidProofOutputs {
		fcr.NewValidProofOutputs[i].UnmarshalSia(d)
	}
	fcr.NewMissedProofOutputs = make([]SiacoinOutput, d.NextPrefix(unsafe.Sizeof(SiacoinOutput{})))
	for i := range fcr.NewMissedProofOutputs {
		fcr.NewMissedProofOutputs[i].UnmarshalSia(d)
	}
	d.ReadFull(fcr.NewUnlockHash[:])
	return d.Err()
}

// MarshalSia implements the encoding.SiaMarshaler interface.
func (sci SiacoinInputV0) MarshalSia(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.Write(sci.ParentID[:])
	sci.UnlockConditions.MarshalSia(e)
	return e.Err()
}

// UnmarshalSia implements the encoding.SiaUnmarshaler interface.
func (sci *SiacoinInputV0) UnmarshalSia(r io.Reader) error {
	d := encoding.NewDecoder(r)
	d.ReadFull(sci.ParentID[:])
	sci.UnlockConditions.UnmarshalSia(d)
	return d.Err()
}

// MarshalSia implements the encoding.SiaMarshaler interface.
func (t TransactionV0) MarshalSia(w io.Writer) error {
	if build.DEBUG {
		// Sanity check: compare against the old encoding
		buf := new(bytes.Buffer)
		encoding.NewEncoder(buf).EncodeAll(
			t.SiacoinInputs,
			t.SiacoinOutputs,
			t.FileContracts,
			t.FileContractRevisions,
			t.StorageProofs,
			t.MinerFees,
			t.ArbitraryData,
			t.TransactionSignatures,
		)
		w = sanityCheckWriter{w, buf}
	}

	e := encoding.NewEncoder(w)
	t.marshalSiaNoSignatures(e)
	e.WriteInt(len((t.TransactionSignatures)))
	for i := range t.TransactionSignatures {
		t.TransactionSignatures[i].MarshalSia(e)
	}
	return e.Err()
}

// MarshalSiaNoSignatures is a helper function for calculating certain hashes
// that do not include the transaction's signatures.
func (t TransactionV0) marshalSiaNoSignatures(w io.Writer) {
	e := encoding.NewEncoder(w)
	e.WriteInt(len((t.SiacoinInputs)))
	for i := range t.SiacoinInputs {
		t.SiacoinInputs[i].MarshalSia(e)
	}
	e.WriteInt(len((t.SiacoinOutputs)))
	for i := range t.SiacoinOutputs {
		t.SiacoinOutputs[i].MarshalSia(e)
	}
	e.WriteInt(len((t.FileContracts)))
	for i := range t.FileContracts {
		t.FileContracts[i].MarshalSia(e)
	}
	e.WriteInt(len((t.FileContractRevisions)))
	for i := range t.FileContractRevisions {
		t.FileContractRevisions[i].MarshalSia(e)
	}
	e.WriteInt(len((t.StorageProofs)))
	for i := range t.StorageProofs {
		t.StorageProofs[i].MarshalSia(e)
	}
	e.WriteInt(len((t.MinerFees)))
	for i := range t.MinerFees {
		t.MinerFees[i].MarshalSia(e)
	}
	e.WriteInt(len((t.ArbitraryData)))
	for i := range t.ArbitraryData {
		e.WritePrefixedBytes(t.ArbitraryData[i])
	}
}

// MarshalSiaNoSignatures is a wrapper used for the miningpool module.
// NOTE: Remove it when will be not needed
func (t TransactionV0) MarshalSiaNoSignatures(w io.Writer) {
	t.marshalSiaNoSignatures(w)
}

// MarshalSiaSize returns the encoded size of t.
func (t TransactionV0) MarshalSiaSize() (size int) {
	size += 8
	for _, sci := range t.SiacoinInputs {
		size += len(sci.ParentID)
		size += sci.UnlockConditions.MarshalSiaSize()
	}
	size += 8
	for _, sco := range t.SiacoinOutputs {
		size += sco.Value.MarshalSiaSize()
		size += len(sco.UnlockHash)
	}
	size += 8
	for i := range t.FileContracts {
		size += t.FileContracts[i].MarshalSiaSize()
	}
	size += 8
	for i := range t.FileContractRevisions {
		size += t.FileContractRevisions[i].MarshalSiaSize()
	}
	size += 8
	for _, sp := range t.StorageProofs {
		size += len(sp.ParentID)
		size += len(sp.Segment)
		size += 8 + len(sp.HashSet)*crypto.HashSize
	}
	size += 8
	for i := range t.MinerFees {
		size += t.MinerFees[i].MarshalSiaSize()
	}
	size += 8
	for i := range t.ArbitraryData {
		size += 8 + len(t.ArbitraryData[i])
	}
	size += 8
	for _, ts := range t.TransactionSignatures {
		size += len(ts.ParentID)
		size += 8 // ts.PublicKeyIndex
		size += 8 // ts.Timelock
		size += ts.CoveredFields.MarshalSiaSize()
		size += 8 + len(ts.Signature)
	}

	// Sanity check against the slower method.
	if build.DEBUG {
		expectedSize := len(encoding.Marshal(t))
		if expectedSize != size {
			panic("Transaction size different from expected size.")
		}
	}
	return
}

// UnmarshalSia implements the encoding.SiaUnmarshaler interface.
func (t *TransactionV0) UnmarshalSia(r io.Reader) error {
	d := encoding.NewDecoder(r)
	t.SiacoinInputs = make([]SiacoinInputV0, d.NextPrefix(unsafe.Sizeof(SiacoinInput{})))
	for i := range t.SiacoinInputs {
		t.SiacoinInputs[i].UnmarshalSia(d)
	}
	t.SiacoinOutputs = make([]SiacoinOutput, d.NextPrefix(unsafe.Sizeof(SiacoinOutput{})))
	for i := range t.SiacoinOutputs {
		t.SiacoinOutputs[i].UnmarshalSia(d)
	}
	t.FileContracts = make([]FileContract, d.NextPrefix(unsafe.Sizeof(FileContract{})))
	for i := range t.FileContracts {
		t.FileContracts[i].UnmarshalSia(d)
	}
	t.FileContractRevisions = make([]FileContractRevisionV0, d.NextPrefix(unsafe.Sizeof(FileContractRevision{})))
	for i := range t.FileContractRevisions {
		t.FileContractRevisions[i].UnmarshalSia(d)
	}
	t.StorageProofs = make([]StorageProof, d.NextPrefix(unsafe.Sizeof(StorageProof{})))
	for i := range t.StorageProofs {
		t.StorageProofs[i].UnmarshalSia(d)
	}
	t.MinerFees = make([]Currency, d.NextPrefix(unsafe.Sizeof(Currency{})))
	for i := range t.MinerFees {
		t.MinerFees[i].UnmarshalSia(d)
	}
	t.ArbitraryData = make([][]byte, d.NextPrefix(unsafe.Sizeof([]byte{})))
	for i := range t.ArbitraryData {
		t.ArbitraryData[i] = d.ReadPrefixedBytes()
	}
	t.TransactionSignatures = make([]TransactionSignature, d.NextPrefix(unsafe.Sizeof(TransactionSignature{})))
	for i := range t.TransactionSignatures {
		t.TransactionSignatures[i].UnmarshalSia(d)
	}
	return d.Err()
}

// MarshalSia implements the encoding.SiaMarshaler interface.
func (uc UnlockConditionsV0) MarshalSia(w io.Writer) error {
	e := encoding.NewEncoder(w)
	e.WriteUint64(uint64(uc.Timelock))
	e.WriteInt(len(uc.PublicKeys))
	for _, spk := range uc.PublicKeys {
		spk.MarshalSia(e)
	}
	e.WriteUint64(uc.SignaturesRequired)
	return e.Err()
}

// MarshalSiaSize returns the encoded size of uc.
func (uc UnlockConditionsV0) MarshalSiaSize() (size int) {
	size += 8 // Timelock
	size += 8 // length prefix for PublicKeys
	for _, spk := range uc.PublicKeys {
		size += len(spk.Algorithm)
		size += 8 + len(spk.Key)
	}
	size += 8 // SignaturesRequired
	return
}

// UnmarshalSia implements the encoding.SiaUnmarshaler interface.
func (uc *UnlockConditionsV0) UnmarshalSia(r io.Reader) error {
	d := encoding.NewDecoder(r)
	uc.Timelock = BlockHeight(d.NextUint64())
	uc.PublicKeys = make([]SiaPublicKey, d.NextPrefix(unsafe.Sizeof(SiaPublicKey{})))
	for i := range uc.PublicKeys {
		uc.PublicKeys[i].UnmarshalSia(d)
	}
	uc.SignaturesRequired = d.NextUint64()
	return d.Err()
}
