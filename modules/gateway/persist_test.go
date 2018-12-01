package gateway

import (
	"reflect"
	"testing"
)

func TestLoad(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Start new gateway
	g := newTestingGateway(t)

	// Add node and persist node and gateway
	g.mu.Lock()
	g.addNode(dummyNode)
	if err := g.saveSync(); err != nil {
		t.Fatal(err)
	}
	if err := g.saveSyncNodes(); err != nil {
		t.Fatal(err)
	}
	g.mu.Unlock()
	g.Close()

	// Start second gateway
	g2, err := New("localhost:0", false, g.persistDir, false)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm node from gateway 1 is in gateway 2
	if _, ok := g2.nodes[dummyNode]; !ok {
		t.Fatal("gateway did not load old peer list:", g2.nodes)
	}

	// Confirm the persisted gateway information is the same between the two
	// gateways
	if !reflect.DeepEqual(g.persist, g2.persist) {
		t.Log("g.persit:", g.persist)
		t.Log("g2.persit:", g2.persist)
		t.Fatal("Gateway not persisted")
	}
}
