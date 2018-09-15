package host

import (
	"net"
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/ed25519"
)

// managedBuildJointKey handles communication between renter and host to generate a joint
// public key. The host is participant 0, the renter is participant 1.
func (h *Host) managedBuildJointKey(conn net.Conn) (ed25519.PrivateKey, error) {
	conn.SetDeadline(time.Now().Add(modules.NegotiatePublicKeyTime))
	var jointPrivateKey ed25519.PrivateKey
	var renterPublicKey ed25519.PublicKey
	err := modules.ReadPublicKey(conn, &renterPublicKey)
	if err != nil {
		return jointPrivateKey, err
	}
	var privateKey ed25519.PrivateKey
	var hostPublicKey ed25519.PublicKey
	copy(privateKey[:], h.secretKey[:])
	copy(hostPublicKey[:], h.publicKey.Key[:])
	publicKeys := []ed25519.PublicKey{hostPublicKey, renterPublicKey}
	jointPrivateKey, err = ed25519.GenerateJointPrivateKey(publicKeys, privateKey, 0)
	if err != nil {
		return jointPrivateKey, err
	}
	err = modules.WritePublicKey(conn, hostPublicKey)
	if err != nil {
		return jointPrivateKey, err
	}
	return jointPrivateKey, err
}
