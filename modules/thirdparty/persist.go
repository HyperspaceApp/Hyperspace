package thirdparty

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siadir"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siafile"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/NebulousLabs/Sia/build"
	"github.com/NebulousLabs/Sia/encoding"

	"github.com/HyperspaceApp/errors"
	"github.com/HyperspaceApp/writeaheadlog"
)

const (
	logFile = modules.ThirdpartyDir + ".log"
	// PersistFilename is the filename to be used when persisting renter
	// information to a JSON file
	PersistFilename = "thirdparty.json"
	// SiaDirMetadata is the name of the metadata file for the sia directory
	SiaDirMetadata = ".hyperspacedir"
	// walFile is the filename of the renter's writeaheadlog's file.
	walFile = modules.ThirdpartyDir + ".wal"
)

var (
	//ErrBadFile is an error when a file does not qualify as .sia file
	ErrBadFile = errors.New("not a .sia file")
	// ErrIncompatible is an error when file is not compatible with current
	// version
	ErrIncompatible = errors.New("file is not compatible with current version")
	// ErrNoNicknames is an error when no nickname is given
	ErrNoNicknames = errors.New("at least one nickname must be supplied")
	// ErrNonShareSuffix is an error when the suffix of a file does not match
	// the defined share extension
	ErrNonShareSuffix = errors.New("suffix of file must be " + siafile.ShareExtension)

	settingsMetadata = persist.Metadata{
		Header:  "Renter Persistence",
		Version: persistVersion,
	}

	shareHeader  = [15]byte{'S', 'i', 'a', ' ', 'S', 'h', 'a', 'r', 'e', 'd', ' ', 'F', 'i', 'l', 'e'}
	shareVersion = "0.4"

	// Persist Version Numbers
	persistVersion040 = "0.4"
	persistVersion133 = "1.3.3"
)

type (
	// persist contains all of the persistent renter data.
	persistence struct {
		MaxDownloadSpeed int64
		MaxUploadSpeed   int64
		StreamCacheSize  uint64
	}
)

// MarshalSia implements the encoding.SiaMarshaller interface, writing the
// file data to w.
func (f *file) MarshalSia(w io.Writer) error {
	enc := encoding.NewEncoder(w)

	// encode easy fields
	err := enc.EncodeAll(
		f.name,
		f.size,
		f.masterKey,
		f.pieceSize,
		f.mode,
	)
	if err != nil {
		return err
	}
	// COMPATv0.4.3 - encode the bytesUploaded and chunksUploaded fields
	// TODO: the resulting .sia file may confuse old clients.
	err = enc.EncodeAll(f.pieceSize*f.numChunks()*uint64(f.erasureCode.NumPieces()), f.numChunks())
	if err != nil {
		return err
	}

	// encode erasureCode
	switch code := f.erasureCode.(type) {
	case *siafile.RSCode:
		err = enc.EncodeAll(
			"Reed-Solomon",
			uint64(code.MinPieces()),
			uint64(code.NumPieces()-code.MinPieces()),
		)
		if err != nil {
			return err
		}
	default:
		if build.DEBUG {
			panic("unknown erasure code")
		}
		return errors.New("unknown erasure code")
	}
	// encode contracts
	if err := enc.Encode(uint64(len(f.contracts))); err != nil {
		return err
	}
	for _, c := range f.contracts {
		if err := enc.Encode(c); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalSia implements the encoding.SiaUnmarshaler interface,
// reconstructing a file from the encoded bytes read from t.
func (f *file) UnmarshalSia(r io.Reader) error {
	dec := encoding.NewDecoder(r)

	// COMPATv0.4.3 - decode bytesUploaded and chunksUploaded into dummy vars.
	var bytesUploaded, chunksUploaded uint64

	// Decode easy fields.
	err := dec.DecodeAll(
		&f.name,
		&f.size,
		&f.masterKey,
		&f.pieceSize,
		&f.mode,
		&bytesUploaded,
		&chunksUploaded,
	)
	if err != nil {
		return err
	}
	f.staticUID = persist.RandomSuffix()

	// Decode erasure codet.
	var codeType string
	if err := dec.Decode(&codeType); err != nil {
		return err
	}
	switch codeType {
	case "Reed-Solomon":
		var nData, nParity uint64
		err = dec.DecodeAll(
			&nData,
			&nParity,
		)
		if err != nil {
			return err
		}
		rsc, err := siafile.NewRSCode(int(nData), int(nParity))
		if err != nil {
			return err
		}
		f.erasureCode = rsc
	default:
		return errors.New("unrecognized erasure code type: " + codeType)
	}

	// Decode contracts.
	var nContracts uint64
	if err := dec.Decode(&nContracts); err != nil {
		return err
	}
	f.contracts = make(map[types.FileContractID]fileContract)
	var contract fileContract
	for i := uint64(0); i < nContracts; i++ {
		if err := dec.Decode(&contract); err != nil {
			return err
		}
		f.contracts[contract.ID] = contract
	}
	return nil
}

// createDir creates directory in the renter directory
func (t *Thirdparty) createDir(hyperspacepath string) error {
	// Enforce nickname rules.
	if err := validateSiapath(hyperspacepath); err != nil {
		return err
	}

	// Create direcotry
	path := filepath.Join(t.filesDir, hyperspacepath)
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}

	// Make sure all parent directories have metadata files
	for path != filepath.Dir(t.filesDir) {
		if err := createDirMetadata(path); err != nil {
			return err
		}
		path = filepath.Dir(path)
	}
	return nil
}

// createDirMetadata makes sure there is a metadata file in the directory and
// updates or creates one as needed
func createDirMetadata(path string) error {
	fullPath := filepath.Join(path, SiaDirMetadata)
	// Check if metadata file exists
	if _, err := os.Stat(fullPath); err == nil {
		// TODO: update metadata file
		return nil
	}

	// TODO: update to get actual min redundancy
	data := struct {
		LastUpdate    int64
		MinRedundancy float64
	}{time.Now().UnixNano(), float64(0)}

	metadataHeader := persist.Metadata{
		Header:  "Sia Directory Metadata",
		Version: persistVersion,
	}

	return persist.SaveJSON(metadataHeader, data, fullPath)
}

// saveSync stores the current renter data to disk and then syncs to disk.
func (t *Thirdparty) saveSync() error {
	// return persist.SaveJSON(settingsMetadata, t.persist, filepath.Join(t.persistDir, PersistFilename))
	return nil
}

// load fetches the saved renter data from disk.
func (t *Thirdparty) loadSettings() error {
	// t.persist = persistence{}
	// err := persist.LoadJSON(settingsMetadata, &t.persist, filepath.Join(t.persistDir, PersistFilename))
	// if os.IsNotExist(err) {
	// 	// No persistence yet, set the defaults and continue.
	// 	t.persist.MaxDownloadSpeed = DefaultMaxDownloadSpeed
	// 	t.persist.MaxUploadSpeed = DefaultMaxUploadSpeed
	// 	t.persist.StreamCacheSize = DefaultStreamCacheSize
	// 	err = t.saveSync()
	// 	if err != nil {
	// 		return err
	// 	}
	// } else if err == persist.ErrBadVersion {
	// 	// Outdated version, try the 040 to 133 upgrade.
	// 	err = convertPersistVersionFrom040To133(filepath.Join(t.persistDir, PersistFilename))
	// 	if err != nil {
	// 		// Nothing left to try.
	// 		return err
	// 	}
	// 	// Re-load the settings now that the file has been upgraded.
	// 	return t.loadSettings()
	// } else if err != nil {
	// 	return err
	// }

	// // Set the bandwidth limits on the contractor, which was already initialized
	// // without bandwidth limits.
	// return t.setBandwidthLimits(t.persist.MaxDownloadSpeed, t.persist.MaxUploadSpeed)
	return nil
}

// initPersist handles all of the persistence initialization, such as creating
// the persistence directory and starting the logget.
func (t *Thirdparty) initPersist() error {
	// Create the persist and files directories if they do not yet exist.
	err := os.MkdirAll(t.filesDir, 0700)
	if err != nil {
		return err
	}

	// Initialize the logget.
	t.log, err = persist.NewFileLogger(filepath.Join(t.persistDir, logFile))
	if err != nil {
		return err
	}

	// Load the prior persistence structures.
	err = t.loadSettings()
	if err != nil {
		return err
	}

	// Initialize the writeaheadlog.
	txns, wal, err := writeaheadlog.New(filepath.Join(t.persistDir, walFile))
	if err != nil {
		return err
	}
	t.wal = wal
	t.staticFileSet = siafile.NewSiaFileSet(t.filesDir, wal)
	t.staticDirSet = siadir.NewSiaDirSet(t.filesDir, wal)

	// Apply unapplied wal txns.
	for _, txn := range txns {
		applyTxn := true
		for _, update := range txn.Updates {
			if siafile.IsSiaFileUpdate(update) {
				if err := siafile.ApplyUpdates(update); err != nil {
					return errors.AddContext(err, "failed to apply SiaFile update")
				}
			} else if siadir.IsSiaDirUpdate(update) {
				if err := siadir.ApplyUpdates(update); err != nil {
					return errors.AddContext(err, "failed to apply SiaDir update")
				}
			} else {
				applyTxn = false
			}
		}
		// If every update of the txn is a SiaFileUpdate (which should be the
		// case since it's either none of them or all) we consider the
		// transaction applied.
		if applyTxn {
			if err := txn.SignalUpdatesApplied(); err != nil {
				return err
			}
		}
	}

	return nil
}
