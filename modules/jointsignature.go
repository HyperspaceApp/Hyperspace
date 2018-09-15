package modules

import (
	"io"

	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/ed25519"
)

// ReadPublicKey reads a public key response from r (usually a
// net.Conn).
func ReadPublicKey(r io.Reader, publicKey *ed25519.PublicKey) error {
	d := encoding.NewDecoder(r)
	d.ReadFull((*publicKey)[:])
	return d.Err()
}

// WritePublicKey writes a public key to w (usually a net.Conn).
func WritePublicKey(w io.Writer, publicKey ed25519.PublicKey) error {
	e := encoding.NewEncoder(w)
	_, err := e.Write(publicKey[:])
	return err
}

// ReadCurvePoint reads a curve point response from r (usually a
// net.Conn).
func ReadCurvePoint(r io.Reader, cp *ed25519.CurvePoint) error {
	d := encoding.NewDecoder(r)
	d.ReadFull((*cp)[:])
	return d.Err()
}

// WriteCurvePoint writes a curve point to w (usually a net.Conn).
func WriteCurvePoint(w io.Writer, curvePoint ed25519.CurvePoint) error {
	e := encoding.NewEncoder(w)
	_, err := e.Write(curvePoint[:])
	return err
}

// ReadSignature reads a signature response from r (usually a
// net.Conn).
func ReadSignature(r io.Reader, sig []byte) error {
	d := encoding.NewDecoder(r)
	d.ReadFull(sig[:])
	return d.Err()
}

// WriteSignature writes a signature to w (usually a net.Conn).
func WriteSignature(w io.Writer, sig []byte) error {
	e := encoding.NewEncoder(w)
	_, err := e.Write(sig[:])
	return err
}
