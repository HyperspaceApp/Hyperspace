package siadir

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/NebulousLabs/writeaheadlog"
)

// equalMetadatas is a helper that compares two siaDirMetadatas.
func equalMetadatas(md, md2 siaDirMetadata) error {
	// Check Health
	if md.Health != md2.Health {
		return fmt.Errorf("healths not equal, %v and %v", md.Health, md2.Health)
	}
	// Check StuckHealth
	if md.StuckHealth != md2.StuckHealth {
		return fmt.Errorf("stuck healths not equal, %v and %v", md.StuckHealth, md2.StuckHealth)
	}
	// Check SiaPath
	if md.SiaPath != md2.SiaPath {
		return fmt.Errorf("siapaths not equal, %v and %v", md.SiaPath, md2.SiaPath)
	}
	// Check RootDir
	if md.RootDir != md2.RootDir {
		return fmt.Errorf("rootDirs not equal, %v and %v", md.RootDir, md2.RootDir)
	}

	return nil
}

// newTestDir creates a new SiaDir for testing, the test Name should be passed
// in as the rootDir
func newTestDir(rootDir string) (*SiaDir, error) {
	rootPath := filepath.Join(os.TempDir(), "siadirs", rootDir)
	if err := os.RemoveAll(rootPath); err != nil {
		return nil, err
	}
	siaPath := string(hex.EncodeToString(fastrand.Bytes(8)))
	wal, _ := newTestWAL()
	return New(siaPath, rootPath, wal)
}

// newTestWal is a helper method to create a WAL for testing.
func newTestWAL() (*writeaheadlog.WAL, string) {
	// Create the wal.
	walsDir := filepath.Join(os.TempDir(), "wals")
	if err := os.MkdirAll(walsDir, 0700); err != nil {
		panic(err)
	}
	walFilePath := filepath.Join(walsDir, hex.EncodeToString(fastrand.Bytes(8)))
	_, wal, err := writeaheadlog.New(walFilePath)
	if err != nil {
		panic(err)
	}
	return wal, walFilePath
}

// TestCreateReadMetadataUpdate tests if an update can be created using createMetadataUpdate
// and if the created update can be read using readMetadataUpdate.
func TestCreateReadMetadataUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sd, err := newTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	// Create metadata update
	update, err := createMetadataUpdate(sd.staticMetadata)
	if err != nil {
		t.Fatal(err)
	}

	// Read metadata update
	metadata, err := readMetadataUpdate(update)
	if err != nil {
		t.Fatal("Failed to read update", err)
	}

	// Compare metadata
	if err := equalMetadatas(metadata, sd.staticMetadata); err != nil {
		t.Fatal(err)
	}
}

// TestCreateReadDeleteUpdate tests if an update can be created using
// createDeleteUpdate and if the created update can be read using
// readDeleteUpdate.
func TestCreateReadDeleteUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	sd, err := newTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	update := sd.createDeleteUpdate()
	// Read update
	path := readDeleteUpdate(update)
	// Compare values
	siaDirPath := filepath.Join(sd.staticMetadata.RootDir, sd.staticMetadata.SiaPath)
	if path != siaDirPath {
		t.Error("paths don't match")
	}
}

// TestApplyUpdates tests a variety of functions that are used to apply
// updates.
func TestApplyUpdates(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	t.Run("TestApplyUpdates", func(t *testing.T) {
		siadir, err := newTestDir(t.Name())
		if err != nil {
			t.Fatal(err)
		}
		testApply(t, siadir, ApplyUpdates)
	})
	t.Run("TestCreateAndApplyTransaction", func(t *testing.T) {
		siadir, err := newTestDir(t.Name())
		if err != nil {
			t.Fatal(err)
		}
		testApply(t, siadir, siadir.createAndApplyTransaction)
	})
}

// testApply tests if a given method applies a set of updates correctly.
func testApply(t *testing.T, siadir *SiaDir, apply func(...writeaheadlog.Update) error) {
	// Create an update to the metadata
	metadata := siadir.staticMetadata
	metadata.Health = 1.0
	update, err := createMetadataUpdate(metadata)
	if err != nil {
		t.Fatal(err)
	}

	// Apply update.
	if err := apply(update); err != nil {
		t.Fatal("Failed to apply update", err)
	}
	// Open file.
	sd, err := LoadSiaDir(metadata.RootDir, metadata.SiaPath, siadir.wal)
	if err != nil {
		t.Fatal("Failed to load siadir", err)
	}
	// Check if correct data was written.
	if err := equalMetadatas(metadata, sd.staticMetadata); err != nil {
		t.Fatal(err)
	}
}
