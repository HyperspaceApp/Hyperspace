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

	// persistFilename is the filename to be used when persisting gateway information to a JSON file
	persistFilename = "gateway.json"
)

// nodePersistMetadata contains the header and version strings that identify the
// node persist file.
var nodePersistMetadata = persist.Metadata{
	Header:  "Sia Node List",
	Version: "1.3.0",
}

// persistMetadata contains the header and version strings that identify the
// gateway persist file.
var persistMetadata = persist.Metadata{
	Header:  "Gateway Persistence",
	Version: "0.2.4",
}

type (
	// persist contains all of the persistent gateway data.
	persistence struct {
		RouterURL string
	}
)

// nodePersistData returns the node data in the Gateway that will be saved to disk.
func (g *Gateway) nodePersistData() (nodes []*node) {
	for _, node := range g.nodes {
		nodes = append(nodes, node)
	}
	return
}

// load loads the Gateway's persistent data from disk.
func (g *Gateway) load() error {
	g.persist = persistence{}
	return persist.LoadJSON(persistMetadata, &g.persist, filepath.Join(g.persistDir, persistFilename))
}

// loadNodes loads the Gateway's persistent node data from disk.
func (g *Gateway) loadNodes() error {
	var nodes []*node
	persist.LoadJSON(nodePersistMetadata, &nodes, filepath.Join(g.persistDir, nodesFile))
	for i := range nodes {
		g.nodes[nodes[i].NetAddress] = nodes[i]
	}
	return nil
}

// saveSync stores the Gateway's persistent data on disk, and then syncs to
// disk to minimize the possibility of data loss.
func (g *Gateway) saveSync() error {
	return persist.SaveJSON(persistMetadata, g.persist, filepath.Join(g.persistDir, persistFilename))
}

// saveSyncNodes stores the Gateway's persistent node data on disk, and then
// syncs to disk to minimize the possibility of data loss.
func (g *Gateway) saveSyncNodes() error {
	return persist.SaveJSON(nodePersistMetadata, g.nodePersistData(), filepath.Join(g.persistDir, nodesFile))
}

// threadedSaveLoop periodically saves the gateway nodes.
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
			err = g.saveSyncNodes()
			g.mu.Unlock()
			if err != nil {
				g.log.Println("ERROR: Unable to save gateway nodes:", err)
			}
		}()
	}
}
