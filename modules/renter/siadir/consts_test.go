package siadir

import (
	"testing"

	"github.com/HyperspaceApp/writeaheadlog"
)

// TestIsSiaDirUpdate tests the IsSiaDirUpdate method.
func TestIsSiaDirUpdate(t *testing.T) {
	sd, err := newTestDir(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	metadataUpdate, err := createMetadataUpdate(siaDirMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	deleteUpdate := sd.createDeleteUpdate()
	emptyUpdate := writeaheadlog.Update{}

	if !IsSiaDirUpdate(metadataUpdate) {
		t.Error("metadataUpdate should be a SiaDirUpdate but wasn't")
	}
	if !IsSiaDirUpdate(deleteUpdate) {
		t.Error("deleteUpdate should be a SiaDirUpdate but wasn't")
	}
	if IsSiaDirUpdate(emptyUpdate) {
		t.Error("emptyUpdate shouldn't be a SiaDirUpdate but was one")
	}
}
