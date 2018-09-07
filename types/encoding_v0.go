package types

import (
	"io"
	"unsafe"

	"github.com/HyperspaceApp/Hyperspace/encoding"
)

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
