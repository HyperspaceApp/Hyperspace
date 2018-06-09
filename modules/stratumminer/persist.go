package stratumminer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/persist"
)

const (
	logFile = modules.StratumMinerDir + ".log"
	//saveLoopPeriod = time.Minute * 2
)

// initPersist initializes the persistence of the miner.
func (sm *StratumMiner) initPersist() error {
	// Create the miner directory.
	err := os.MkdirAll(sm.persistDir, 0700)
	if err != nil {
		return err
	}

	// Add a logger.
	sm.log, err = persist.NewFileLogger(filepath.Join(sm.persistDir, logFile))
	if err != nil {
		return err
	}
	sm.tg.AfterStop(func() {
		if err := sm.log.Close(); err != nil {
			fmt.Errorf("log.Close failed: %v", err)
		}
	})
	return nil
}
