package siadir

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/errors"
	"github.com/HyperspaceApp/writeaheadlog"
)

// ApplyUpdates  applies a number of writeaheadlog updates to the corresponding
// SiaDir. This method can apply updates from different SiaDirs and should only
// be run before the SiaDirs are loaded from disk right after the startup of
// siad. Otherwise we might run into concurrency issues.
func ApplyUpdates(updates ...writeaheadlog.Update) error {
	// Apply updates.
	for _, u := range updates {
		err := applyUpdate(u)
		if err != nil {
			return errors.AddContext(err, "failed to apply update")
		}
	}
	return nil
}

// applyUpdate applies the wal update
func applyUpdate(update writeaheadlog.Update) error {
	switch update.Name {
	case updateDeleteName:
		// Delete from disk
		err := os.RemoveAll(readDeleteUpdate(update))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	case updateMetadataName:
		// Decode update.
		metadata, err := readMetadataUpdate(update)
		if err != nil {
			return err
		}
		// Write data.
		if err := metadata.save(); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("Update not recognized: %v", update.Name)
	}
}

// readDeleteUpdate unmarshals the update's instructions and returns the
// encoded path.
func readDeleteUpdate(update writeaheadlog.Update) string {
	if update.Name != updateDeleteName {
		err := errors.New("readDeleteUpdate can't read non-delete updates")
		build.Critical(err)
		return ""
	}
	return string(update.Instructions)
}

// createDirMetadataAll creates a path on disk to the provided siaPath and make
// sure that all the parent directories have metadata files.
func createDirMetadataAll(siaPath, rootDir string) ([]writeaheadlog.Update, error) {
	// Create path to directory
	if err := os.MkdirAll(filepath.Join(rootDir, siaPath), 0700); err != nil {
		return nil, err
	}

	// Create metadata
	var updates []writeaheadlog.Update
	for {
		siaPath = filepath.Dir(siaPath)
		if siaPath == "." {
			siaPath = ""
		}
		_, update, err := createDirMetadata(siaPath, rootDir)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(update, writeaheadlog.Update{}) {
			updates = append(updates, update)
		}
		if siaPath == "" {
			break
		}
	}
	return updates, nil
}

// createMetadataUpdate is a helper method which creates a writeaheadlog update for
// updating the siaDir metadata
func createMetadataUpdate(metadata siaDirMetadata) (writeaheadlog.Update, error) {
	// Marshal the metadata.
	instructions, err := json.Marshal(metadata)
	if err != nil {
		return writeaheadlog.Update{}, err
	}

	// Create update
	return writeaheadlog.Update{
		Name:         updateMetadataName,
		Instructions: instructions,
	}, nil
}

// readMetadataUpdate unmarshals the update's instructions and returns the
// metadata encoded in the instructions.
func readMetadataUpdate(update writeaheadlog.Update) (metadata siaDirMetadata, err error) {
	if update.Name != updateMetadataName {
		err = errors.New("readMetadataUpdate can't read non-metadata updates")
		build.Critical(err)
		return
	}
	err = json.Unmarshal(update.Instructions, &metadata)
	return
}

// createAndApplyTransaction is a helper method that creates a writeaheadlog
// transaction and applies it.
func (sd *SiaDir) createAndApplyTransaction(updates ...writeaheadlog.Update) error {
	// This should never be called on a deleted directory.
	if sd.deleted {
		return errors.New("shouldn't apply updates on deleted directory")
	}
	// Create the writeaheadlog transaction.
	txn, err := sd.wal.NewTransaction(updates)
	if err != nil {
		return errors.AddContext(err, "failed to create wal txn")
	}
	// No extra setup is required. Signal that it is done.
	if err := <-txn.SignalSetupComplete(); err != nil {
		return errors.AddContext(err, "failed to signal setup completion")
	}
	// Apply the updates.
	if err := ApplyUpdates(updates...); err != nil {
		return errors.AddContext(err, "failed to apply updates")
	}
	// Updates are applied. Let the writeaheadlog know.
	if err := txn.SignalUpdatesApplied(); err != nil {
		return errors.AddContext(err, "failed to signal that updates are applied")
	}
	return nil
}

// createDeleteUpdate is a helper method that creates a writeaheadlog for
// deleting a directory.
func (sd *SiaDir) createDeleteUpdate() writeaheadlog.Update {
	return writeaheadlog.Update{
		Name:         updateDeleteName,
		Instructions: []byte(filepath.Join(sd.staticMetadata.RootDir, sd.staticMetadata.SiaPath)),
	}
}

// saveDir saves the whole SiaDir atomically.
func (sd *SiaDir) saveDir() error {
	metadataUpdates, err := sd.saveMetadataUpdate()
	if err != nil {
		return err
	}
	return sd.createAndApplyTransaction(metadataUpdates)
}

// saveMetadataUpdate saves the metadata of the SiaDir
func (sd *SiaDir) saveMetadataUpdate() (writeaheadlog.Update, error) {
	return createMetadataUpdate(sd.staticMetadata)
}
