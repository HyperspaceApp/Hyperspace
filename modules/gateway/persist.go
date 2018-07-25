package gateway

import (
	"path/filepath"
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
)

const (
	// logFile is the name of the log file.
	logFile = modules.GatewayDir + ".log"

	// nodesFile is the name of the file that contains all seen nodes.
	nodesFile = "nodes.json"
)

// persistMetadata contains the header and version strings that identify the
// gateway persist file.
var persistMetadata = persist.Metadata{
	Header:  "Sia Node List",
	Version: "0.1.0",
}

// persistData returns the data in the Gateway that will be saved to disk.
func (g *Gateway) persistData() (nodes []*node) {
	for _, node := range g.nodes {
		nodes = append(nodes, node)
	}
	return
}

// load loads the Gateway's persistent data from disk.
func (g *Gateway) load() error {
	var nodes []*node
	persist.LoadJSON(persistMetadata, &nodes, filepath.Join(g.persistDir, nodesFile))
	for i := range nodes {
		g.nodes[nodes[i].NetAddress] = nodes[i]
	}
	return nil
}

// saveSync stores the Gateway's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (g *Gateway) saveSync() error {
	return persist.SaveJSON(persistMetadata, g.persistData(), filepath.Join(g.persistDir, nodesFile))
}

// threadedSaveLoop periodically saves the gateway.
func (g *Gateway) threadedSaveLoop() {
	for {
		select {
		case <-g.threads.StopChan():
			return
		case <-time.After(saveFrequency):
		}

		func() {
			err := g.threads.Add()
			if err != nil {
				return
			}
			defer g.threads.Done()

			g.mu.Lock()
			err = g.saveSync()
			g.mu.Unlock()
			if err != nil {
				g.log.Println("ERROR: Unable to save gateway persist:", err)
			}
		}()
	}
}
