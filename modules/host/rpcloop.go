package host

import (
	"crypto/cipher"
	"errors"
	"net"
	"time"

	"github.com/coreos/bbolt"
	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/fastrand"
	"golang.org/x/crypto/chacha20poly1305"
)

// An rpcSession contains the state of an RPC session with a renter.
type rpcSession struct {
	conn net.Conn
	aead cipher.AEAD
	so   storageObligation
}

// extendDeadline extends the read/write deadline on the underlying connection
// by d.
func (s *rpcSession) extendDeadline(d time.Duration) {
	s.conn.SetDeadline(time.Now().Add(d))
}

// readRequest reads an encrypted RPC request from the renter.
func (s *rpcSession) readRequest(resp interface{}) error {
	return modules.ReadRPCRequest(s.conn, s.aead, resp, 1e6)
}

// readResponse reads an encrypted RPC response from the renter.
func (s *rpcSession) readResponse(resp interface{}) error {
	return modules.ReadRPCResponse(s.conn, s.aead, resp, 1e6)
}

// writeResponse sends an encrypted RPC response to the renter.
func (s *rpcSession) writeResponse(resp interface{}) error {
	return modules.WriteRPCResponse(s.conn, s.aead, resp, nil)
}

// writeError sends an encrypted RPC error to the renter.
func (s *rpcSession) writeError(err error) error {
	return modules.WriteRPCResponse(s.conn, s.aead, nil, err)
}

// managedRPCLoop reads new RPCs from the renter, each consisting of a single
// request and response. The loop terminates when the an RPC encounters an
// error or the renter sends modules.RPCLoopExit.
func (h *Host) managedRPCLoop(conn net.Conn) error {
	// read renter's half of key exchange
	conn.SetDeadline(time.Now().Add(rpcRequestInterval))
	var req modules.LoopKeyExchangeRequest
	if err := encoding.NewDecoder(conn).Decode(&req); err != nil {
		return err
	}

	// check for a supported cipher
	var supportsChaCha bool
	for _, c := range req.Ciphers {
		if c == modules.CipherChaCha20Poly1305 {
			supportsChaCha = true
		}
	}
	if !supportsChaCha {
		encoding.NewEncoder(conn).Encode(modules.LoopKeyExchangeResponse{
			Cipher: modules.CipherNoOverlap,
		})
		return errors.New("no supported ciphers")
	}

	// generate a session key, sign it, and derive the shared secret
	xsk, xpk := crypto.GenerateX25519KeyPair()
	pubkeySig := crypto.SignHash(crypto.HashAll(req.PublicKey, xpk), h.secretKey)
	cipherKey := crypto.DeriveSharedSecret(xsk, req.PublicKey)

	// send our half of the key exchange
	resp := modules.LoopKeyExchangeResponse{
		Cipher:    modules.CipherChaCha20Poly1305,
		PublicKey: xpk,
		Signature: pubkeySig[:],
	}
	if err := encoding.NewEncoder(conn).Encode(resp); err != nil {
		return err
	}

	// use cipherKey to initialize an AEAD cipher
	aead, err := chacha20poly1305.New(cipherKey[:])
	if err != nil {
		build.Critical("could not create cipher")
		return err
	}
	// create the session object
	s := &rpcSession{
		conn: conn,
		aead: aead,
	}

	// send encrypted challenge
	var challenge [16]byte
	fastrand.Read(challenge[:])
	challengeReq := modules.LoopChallengeRequest{
		Challenge: challenge,
	}
	if err := s.writeResponse(challengeReq); err != nil {
		return err
	}

	// read encrypted version, contract ID, and challenge response
	//
	// NOTE: if we encounter an error before reading the renter's first RPC,
	// we send it to the renter and close the connection immediately. From the
	// renter's perspective, this error may arrive either before or after
	// sending their first RPC request.
	var challengeResp modules.LoopChallengeResponse
	if err := s.readResponse(&challengeResp); err != nil {
		s.writeError(err)
		return err
	}

	// check handshake version and ciphers
	if challengeResp.Version != 1 {
		err := errors.New("protocol version not supported")
		s.writeError(err)
		return err
	}

	// if a contract was supplied, look it up, verify the challenge response,
	// and lock the storage obligation
	if fcid := challengeResp.ContractID; fcid != (types.FileContractID{}) {
		// look up the renter's public key
		var err error
		h.mu.RLock()
		err = h.db.View(func(tx *bolt.Tx) error {
			s.so, err = getStorageObligation(tx, fcid)
			return err
		})
		h.mu.RUnlock()
		if err != nil {
			s.writeError(errors.New("no record of that contract"))
			return extendErr("could not lock contract "+fcid.String()+": ", err)
		}

		// verify the challenge response
		rev := s.so.RevisionTransactionSet[len(s.so.RevisionTransactionSet)-1].FileContractRevisions[0]
		hash := crypto.HashAll(modules.RPCChallengePrefix, challenge)
		var renterPK crypto.PublicKey
		var renterSig crypto.Signature
		copy(renterPK[:], rev.UnlockConditions.PublicKeys[0].Key)
		copy(renterSig[:], challengeResp.Signature)
		if crypto.VerifyHash(hash, renterPK, renterSig) != nil {
			err := errors.New("challenge signature is invalid")
			s.writeError(err)
			return err
		}

		// lock the storage obligation until the end of the RPC loop
		if err := h.managedTryLockStorageObligation(fcid); err != nil {
			s.writeError(err)
			return extendErr("could not lock contract "+fcid.String()+": ", err)
		}
		defer h.managedUnlockStorageObligation(fcid)
	}

	// enter RPC loop
	for {
		conn.SetDeadline(time.Now().Add(rpcRequestInterval))

		id, err := modules.ReadRPCID(conn, aead)
		if err != nil {
			h.log.Debugf("WARN: could not read RPC ID: %v", err)
			s.writeError(err) // try to write, even though this is probably due to a faulty connection
			return err
		}

		switch id {
		case modules.RPCLoopSettings:
			err = extendErr("incoming RPCLoopSettings failed: ", h.managedRPCLoopSettings(s))
		case modules.RPCLoopFormContract:
			err = extendErr("incoming RPCLoopFormContract failed: ", h.managedRPCLoopFormContract(s))
		case modules.RPCLoopRenewContract:
			err = extendErr("incoming RPCLoopRenewContract failed: ", h.managedRPCLoopRenewContract(s))
		case modules.RPCLoopRecentRevision:
			err = extendErr("incoming RPCLoopRecentRevision failed: ", h.managedRPCLoopRecentRevision(s))
		case modules.RPCLoopUpload:
			err = extendErr("incoming RPCLoopUpload failed: ", h.managedRPCLoopUpload(s))
		case modules.RPCLoopDownload:
			err = extendErr("incoming RPCLoopDownload failed: ", h.managedRPCLoopDownload(s))
		case modules.RPCLoopSectorRoots:
			err = extendErr("incoming RPCLoopSectorRoots failed: ", h.managedRPCLoopSectorRoots(s))
		case modules.RPCLoopExit:
			return nil
		default:
			return errors.New("invalid or unknown RPC ID: " + id.String())
		}
		if err != nil {
			return err
		}
	}
}
