package thirdpartyrenter

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siadir"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siafile"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/HyperspaceApp/errors"
	"github.com/HyperspaceApp/writeaheadlog"
)

const (
	logFile = modules.ThirdpartyRenterDir + ".log"
	// PersistFilename is the filename to be used when persisting renter
	// information to a JSON file
	PersistFilename = "thirdpartyrenter.json"
	// SiaDirMetadata is the name of the metadata file for the sia directory
	SiaDirMetadata = ".siadir"
	// walFile is the filename of the renter's writeaheadlog's file.
	walFile = modules.ThirdpartyRenterDir + ".wal"
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
// reconstructing a file from the encoded bytes read from r.
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

	// Decode erasure coder.
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
func (r *ThirdpartyRenter) createDir(hyperspacepath string) error {
	// Enforce nickname rules.
	if err := validateSiapath(hyperspacepath); err != nil {
		return err
	}

	// Create direcotry
	path := filepath.Join(r.filesDir, hyperspacepath)
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}

	// Make sure all parent directories have metadata files
	for path != filepath.Dir(r.filesDir) {
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
func (r *ThirdpartyRenter) saveSync() error {
	return persist.SaveJSON(settingsMetadata, r.persist, filepath.Join(r.persistDir, PersistFilename))
}

// load fetches the saved renter data from disk.
func (r *ThirdpartyRenter) loadSettings() error {
	r.persist = persistence{}
	err := persist.LoadJSON(settingsMetadata, &r.persist, filepath.Join(r.persistDir, PersistFilename))
	if os.IsNotExist(err) {
		// No persistence yet, set the defaults and continue.
		r.persist.MaxDownloadSpeed = DefaultMaxDownloadSpeed
		r.persist.MaxUploadSpeed = DefaultMaxUploadSpeed
		r.persist.StreamCacheSize = DefaultStreamCacheSize
		err = r.saveSync()
		if err != nil {
			return err
		}
	} else if err == persist.ErrBadVersion {
		// Outdated version, try the 040 to 133 upgrade.
		err = convertPersistVersionFrom040To133(filepath.Join(r.persistDir, PersistFilename))
		if err != nil {
			// Nothing left to try.
			return err
		}
		// Re-load the settings now that the file has been upgraded.
		return r.loadSettings()
	} else if err != nil {
		return err
	}

	// Set the bandwidth limits on the contractor, which was already initialized
	// without bandwidth limits.
	return r.setBandwidthLimits(r.persist.MaxDownloadSpeed, r.persist.MaxUploadSpeed)
}

// shareFiles writes the specified files to w. First a header is written,
// followed by the gzipped concatenation of each file.
func shareFiles(files []*file, w io.Writer) error {
	// Write header.
	err := encoding.NewEncoder(w).EncodeAll(
		shareHeader,
		shareVersion,
		uint64(len(files)),
	)
	if err != nil {
		return err
	}

	// Create compressor.
	zip, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	enc := encoding.NewEncoder(zip)

	// Encode each file.
	for _, f := range files {
		err = enc.Encode(f)
		if err != nil {
			return err
		}
	}

	return zip.Close()
}

/*
// ShareFiles saves the specified files to shareDest.
func (r *ThirdpartyRenter) ShareFiles(nicknames []string, shareDest string) error {
	lockID := r.mu.RLock()
	defer r.mu.RUnlock(lockID)

	// TODO: consider just appending the proper extension.
	if filepath.Ext(shareDest) != siafile.ShareExtension {
		return ErrNonShareSuffix
	}

	handle, err := os.Create(shareDest)
	if err != nil {
		return err
	}
	defer handle.Close()
	sfs, err := r.staticFileSet.All()
	if err != nil {
		return err
	}

	// Load files from renter.
	files := make([]*file, len(nicknames))
	for i, name := range nicknames {
		exists := r.files[name]
		if !exists {
			return siafile.ErrUnknownPath
		}
		files[i] = f
	}

	err = shareFiles(files, handle)
	if err != nil {
		os.Remove(shareDest)
		return err
	}

	return nil
}

// ShareFilesASCII returns the specified files in ASCII format.
func (r *ThirdpartyRenter) ShareFilesASCII(nicknames []string) (string, error) {
	lockID := r.mu.RLock()
	defer r.mu.RUnlock(lockID)

	// Load files from renter.
	files := make([]*file, len(nicknames))
	for i, name := range nicknames {
		f, exists := r.files[name]
		if !exists {
			return "", siafile.ErrUnknownPath
		}
		files[i] = f
	}

	buf := new(bytes.Buffer)
	err := shareFiles(files, base64.NewEncoder(base64.URLEncoding, buf))
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
*/

// loadSharedFiles reads .sia data from reader and registers the contained
// files in the renter. It returns the nicknames of the loaded files.
func (r *ThirdpartyRenter) loadSharedFiles(reader io.Reader, repairPath string) ([]string, error) {
	// read header
	var header [15]byte
	var version string
	var numFiles uint64
	err := encoding.NewDecoder(reader).DecodeAll(
		&header,
		&version,
		&numFiles,
	)
	if err != nil {
		return nil, err
	} else if header != shareHeader {
		return nil, ErrBadFile
	} else if version != shareVersion {
		return nil, ErrIncompatible
	}

	// Create decompressor.
	unzip, err := gzip.NewReader(reader)
	if err != nil {
		return nil, err
	}
	dec := encoding.NewDecoder(unzip)

	// Read each file.
	files := make([]*file, numFiles)
	for i := range files {
		files[i] = new(file)
		err := dec.Decode(files[i])
		if err != nil {
			return nil, err
		}

		// Make sure the file's name does not conflict with existing files.
		dupCount := 0
		origName := files[i].name
		for {
			_, err := r.staticFileSet.Exists(files[i].name)
			if os.IsNotExist(err) {
				break
			}
			dupCount++
			files[i].name = origName + "_" + strconv.Itoa(dupCount)
		}
	}

	// Add files to renter.
	names := make([]string, numFiles)
	for i, f := range files {
		// fileToSiaFile adds siafile to the SiaFileSet so it does not need to
		// be returned here
		siafilePath := filepath.Join(r.filesDir, f.name)
		entry, err := r.fileToSiaFile(f, siafilePath)
		if err != nil {
			return nil, err
		}
		names[i] = f.name
		err = errors.Compose(err, entry.Close())
	}
	// TODO Save the file in the new format.
	return names, err
}

// initPersist handles all of the persistence initialization, such as creating
// the persistence directory and starting the logger.
func (r *ThirdpartyRenter) initPersist() error {
	// Create the persist and files directories if they do not yet exist.
	err := os.MkdirAll(r.filesDir, 0700)
	if err != nil {
		return err
	}

	// Initialize the logger.
	r.log, err = persist.NewFileLogger(filepath.Join(r.persistDir, logFile))
	if err != nil {
		return err
	}

	// Load the prior persistence structures.
	err = r.loadSettings()
	if err != nil {
		return err
	}

	// Initialize the writeaheadlog.
	txns, wal, err := writeaheadlog.New(filepath.Join(r.persistDir, walFile))
	if err != nil {
		return err
	}
	r.wal = wal
	r.staticFileSet = siafile.NewSiaFileSet(r.filesDir, wal)
	r.staticDirSet = siadir.NewSiaDirSet(r.filesDir, wal)

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

// LoadSharedFiles loads a .sia file into the renter. It returns the nicknames
// of the loaded files.
func (r *ThirdpartyRenter) LoadSharedFiles(filename string) ([]string, error) {
	lockID := r.mu.Lock()
	defer r.mu.Unlock(lockID)

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return r.loadSharedFiles(file, filename)
}

// LoadSharedFilesASCII loads an ASCII-encoded .sia file into the renter. It
// returns the nicknames of the loaded files.
func (r *ThirdpartyRenter) LoadSharedFilesASCII(asciiSia string) ([]string, error) {
	lockID := r.mu.Lock()
	defer r.mu.Unlock(lockID)

	dec := base64.NewDecoder(base64.URLEncoding, bytes.NewBufferString(asciiSia))
	return r.loadSharedFiles(dec, "")
}

// convertPersistVersionFrom040to133 upgrades a legacy persist file to the next
// version, adding new fields with their default values.
func convertPersistVersionFrom040To133(path string) error {
	metadata := persist.Metadata{
		Header:  settingsMetadata.Header,
		Version: persistVersion040,
	}
	p := persistence{}

	err := persist.LoadJSON(metadata, &p, path)
	if err != nil {
		return err
	}
	metadata.Version = persistVersion133
	p.MaxDownloadSpeed = DefaultMaxDownloadSpeed
	p.MaxUploadSpeed = DefaultMaxUploadSpeed
	p.StreamCacheSize = DefaultStreamCacheSize
	return persist.SaveJSON(metadata, p, path)
}
