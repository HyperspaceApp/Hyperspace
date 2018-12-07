package renter

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siafile"

	"github.com/HyperspaceApp/fastrand"
)

// newTestingFile initializes a file object with random parameters.
func newTestingFile() *siafile.SiaFile {
	data := fastrand.Bytes(8)
	nData := fastrand.Intn(10)
	nParity := fastrand.Intn(10)
	rsc, _ := siafile.NewRSCode(nData+1, nParity+1)

	name := "testfile-" + strconv.Itoa(int(data[0]))

	return newFileTesting(name, newTestingWal(), rsc, 1000, 0777, "")
}

// equalFiles is a helper function that compares two files for equality.
func equalFiles(f1, f2 *siafile.SiaFile) error {
	if f1 == nil || f2 == nil {
		return fmt.Errorf("one or both files are nil")
	}
	if f1.HyperspacePath() != f2.HyperspacePath() {
		return fmt.Errorf("names do not match: %v %v", f1.HyperspacePath(), f2.HyperspacePath())
	}
	if f1.Size() != f2.Size() {
		return fmt.Errorf("sizes do not match: %v %v", f1.Size(), f2.Size())
	}
	mk1 := f1.MasterKey()
	mk2 := f2.MasterKey()
	if !bytes.Equal(mk1.Key(), mk2.Key()) {
		return fmt.Errorf("keys do not match: %v %v", mk1.Key(), mk2.Key())
	}
	if f1.PieceSize() != f2.PieceSize() {
		return fmt.Errorf("pieceSizes do not match: %v %v", f1.PieceSize(), f2.PieceSize())
	}
	return nil
}

// TestRenterSaveLoad probes the save and load methods of the renter type.
func TestRenterSaveLoad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	// Check that the default values got set correctly.
	settings := rt.renter.Settings()
	if settings.MaxDownloadSpeed != DefaultMaxDownloadSpeed {
		t.Error("default max download speed not set at init")
	}
	if settings.MaxUploadSpeed != DefaultMaxUploadSpeed {
		t.Error("default max upload speed not set at init")
	}
	if settings.StreamCacheSize != DefaultStreamCacheSize {
		t.Error("default stream cache size not set at init")
	}

	// Update the settings of the renter to have a new stream cache size and
	// download speed.
	newDownSpeed := int64(300e3)
	newUpSpeed := int64(500e3)
	newCacheSize := uint64(3)
	settings.MaxDownloadSpeed = newDownSpeed
	settings.MaxUploadSpeed = newUpSpeed
	settings.StreamCacheSize = newCacheSize
	rt.renter.SetSettings(settings)

	err = rt.renter.saveSync() // save metadata
	if err != nil {
		t.Fatal(err)
	}
	err = rt.renter.Close()
	if err != nil {
		t.Fatal(err)
	}

	// load should now load the files into memory.
	rt.renter, err = New(rt.gateway, rt.cs, rt.wallet, rt.tpool, filepath.Join(rt.dir, modules.RenterDir))
	if err != nil {
		t.Fatal(err)
	}

	newSettings := rt.renter.Settings()
	if newSettings.MaxDownloadSpeed != newDownSpeed {
		t.Error("download settings not being persisted correctly")
	}
	if newSettings.MaxUploadSpeed != newUpSpeed {
		t.Error("upload settings not being persisted correctly")
	}
	if newSettings.StreamCacheSize != newCacheSize {
		t.Error("cache settings not being persisted correctly")
	}
}

// TestRenterDirectories checks that the renter properly created metadata files
// for direcotries
func TestRenterDirectories(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	// Test creating directory
	err = rt.renter.CreateDir("foo/bar/baz")
	if err != nil {
		t.Fatal(err)
	}

	// Confirm that direcotry metadata files were created in all directories.
	if _, err = os.Stat(filepath.Join(rt.renter.persistDir, SiaDirMetadata)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(rt.renter.persistDir, "foo/"+SiaDirMetadata)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(rt.renter.persistDir, "foo/bar/"+SiaDirMetadata)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(rt.renter.persistDir, "foo/bar/baz/"+SiaDirMetadata)); err != nil {
		t.Fatal(err)
	}
}

// TestRenterPaths checks that the renter properly handles nicknames
// containing the path separator ("/").
func TestRenterPaths(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	// Create and save some files.
	// The result of saving these files should be a directory containing:
	//   foo.sia
	//   foo/bar.sia
	//   foo/bar/baz.sia
	f1 := newTestingFile()
	f1.Rename("foo", filepath.Join(rt.renter.persistDir, "foo"+ShareExtension))
	f2 := newTestingFile()
	f2.Rename("foo/bar", filepath.Join(rt.renter.persistDir, "foo/bar"+ShareExtension))
	f3 := newTestingFile()
	f3.Rename("foo/bar/baz", filepath.Join(rt.renter.persistDir, "foo/bar/baz"+ShareExtension))

	// Restart the renter to re-do the init cycle.
	err = rt.renter.Close()
	if err != nil {
		t.Fatal(err)
	}
	rt.renter, err = New(rt.gateway, rt.cs, rt.wallet, rt.tpool, filepath.Join(rt.dir, modules.RenterDir))
	if err != nil {
		t.Fatal(err)
	}

	// Check that the files were loaded properly.
	if err := equalFiles(f1, rt.renter.files[f1.HyperspacePath()]); err != nil {
		t.Fatal(err)
	}
	if err := equalFiles(f2, rt.renter.files[f2.HyperspacePath()]); err != nil {
		t.Fatal(err)
	}
	if err := equalFiles(f3, rt.renter.files[f3.HyperspacePath()]); err != nil {
		t.Fatal(err)
	}

	// To confirm that the file structure was preserved, we walk the renter
	// folder and emit the name of each .sia file encountered (filepath.Walk
	// is deterministic; it orders the files lexically).
	var walkStr string
	filepath.Walk(rt.renter.persistDir, func(path string, _ os.FileInfo, _ error) error {
		// capture only .sia files
		if filepath.Ext(path) != ".sia" {
			return nil
		}
		rel, _ := filepath.Rel(rt.renter.persistDir, path) // strip testdir prefix
		walkStr += rel
		return nil
	})
	// walk will descend into foo/bar/, reading baz, bar, and finally foo
	expWalkStr := (f3.HyperspacePath() + ".sia") + (f2.HyperspacePath() + ".sia") + (f1.HyperspacePath() + ".sia")
	if filepath.ToSlash(walkStr) != expWalkStr {
		t.Fatalf("Bad walk string: expected %v, got %v", expWalkStr, walkStr)
	}
}

// TestSiafileCompatibility tests that the renter is able to load v0.4.8 .sia files.
func TestSiafileCompatibility(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	rt, err := newRenterTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	// Load the compatibility file into the renter.
	path := filepath.Join("..", "..", "compatibility", "siafile_v0.4.8.sia")
	names, err := rt.renter.LoadSharedFiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "testfile-183" {
		t.Fatal("nickname not loaded properly:", names)
	}
}
