package gateway

import (
	"testing"

	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/persist"
)

func TestLoad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	g := newTestingGateway(t)

	g.mu.Lock()
	g.addNode(dummyNode)
	g.saveSync()
	g.mu.Unlock()
	g.Close()

	g2, err := New("localhost:0", false, g.persistDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := g2.nodes[dummyNode]; !ok {
		t.Fatal("gateway did not load old peer list:", g2.nodes)
	}
}
