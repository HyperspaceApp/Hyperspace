package siatest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HyperspaceApp/Hyperspace/node"
	"github.com/HyperspaceApp/Hyperspace/node/api/client"
	"github.com/HyperspaceApp/Hyperspace/node/api/server"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/HyperspaceApp/errors"
)

// TestNode is a helper struct for testing that contains a server and a client
// as embedded fields.
type TestNode struct {
	*server.Server
	client.Client
	params      node.NodeParams
	primarySeed string

	downloadDir *LocalDir
	uploadDir   *LocalDir
}

// PrintDebugInfo prints out helpful debug information when debug tests and ndfs, the
// boolean arguments dictate what is printed
func (tn *TestNode) PrintDebugInfo(t *testing.T, contractInfo, hostInfo, renterInfo bool) {
	if contractInfo {
		rc, err := tn.RenterInactiveContractsGet()
		if err != nil {
			t.Log(err)
		}
		t.Log("Active Contracts")
		for _, c := range rc.ActiveContracts {
			t.Log("    ID", c.ID)
			t.Log("    HostPublicKey", c.HostPublicKey)
			t.Log("    GoodForUpload", c.GoodForUpload)
			t.Log("    GoodForRenew", c.GoodForRenew)
			t.Log("    EndHeight", c.EndHeight)
		}
		t.Log()
		t.Log("Inactive Contracts")
		for _, c := range rc.InactiveContracts {
			t.Log("    ID", c.ID)
			t.Log("    HostPublicKey", c.HostPublicKey)
			t.Log("    GoodForUpload", c.GoodForUpload)
			t.Log("    GoodForRenew", c.GoodForRenew)
			t.Log("    EndHeight", c.EndHeight)
		}
		t.Log()
		rce, err := tn.RenterExpiredContractsGet()
		if err != nil {
			t.Log(err)
		}
		t.Log("Expired Contracts")
		for _, c := range rce.ExpiredContracts {
			t.Log("    ID", c.ID)
			t.Log("    HostPublicKey", c.HostPublicKey)
			t.Log("    GoodForUpload", c.GoodForUpload)
			t.Log("    GoodForRenew", c.GoodForRenew)
			t.Log("    EndHeight", c.EndHeight)
		}
		t.Log()
	}

	if hostInfo {
		hdbag, err := tn.HostDbAllGet()
		if err != nil {
			t.Log(err)
		}
		t.Log("Active Hosts from HostDB")
		for _, host := range hdbag.Hosts {
			t.Log("    Host:", host.NetAddress)
			t.Log("        pk", host.PublicKey)
			t.Log("        Accepting Contracts", host.HostExternalSettings.AcceptingContracts)
			t.Log("        Filtered", host.Filtered)
			t.Log("        LastIPNetChange", host.LastIPNetChange.String())
			t.Log("        Subnets")
			for _, subnet := range host.IPNets {
				t.Log("            ", subnet)
			}
		}
		t.Log()
	}

	if renterInfo {
		rg, err := tn.RenterGet()
		if err != nil {
			t.Log(err)
		}
		t.Log("CP:", rg.CurrentPeriod)
		cg, err := tn.ConsensusGet()
		if err != nil {
			t.Log(err)
		}
		t.Log("BH:", cg.Height)
		settings := rg.Settings
		t.Log("Allowance Funds:", settings.Allowance.Funds.HumanString())
		fm := rg.FinancialMetrics
		t.Log("Unspent Funds:", fm.Unspent.HumanString())
		t.Log()
	}
}

// RestartNode restarts a TestNode
func (tn *TestNode) RestartNode() error {
	err := tn.StopNode()
	if err != nil {
		return errors.AddContext(err, "Could not stop node")
	}
	err = tn.StartNode()
	if err != nil {
		return errors.AddContext(err, "Could not start node")
	}
	return nil
}

// StartNode starts a TestNode from an active group
func (tn *TestNode) StartNode() error {
	// Create server
	s, err := server.New(":0", tn.UserAgent, tn.Password, tn.params)
	if err != nil {
		return err
	}
	tn.Server = s
	tn.Client.Address = s.APIAddress()
	return tn.WalletUnlockPost(tn.primarySeed)
}

// StopNode stops a TestNode
func (tn *TestNode) StopNode() error {
	return errors.AddContext(tn.Close(), "failed to stop node")
}

// NewNode creates a new funded TestNode
func NewNode(nodeParams node.NodeParams) (*TestNode, error) {
	// We can't create a funded node without a miner
	if !nodeParams.CreateMiner && nodeParams.Miner == nil {
		return nil, errors.New("Can't create funded node without miner")
	}
	// Create clean node
	tn, err := NewCleanNode(nodeParams)
	if err != nil {
		return nil, err
	}
	// Fund the node
	for i := types.BlockHeight(0); i <= types.MaturityDelay; i++ {
		if err := tn.MineBlock(); err != nil {
			return nil, err
		}
	}
	// Return TestNode
	return tn, nil
}

// NewCleanNode creates a new TestNode that's not yet funded
func NewCleanNode(nodeParams node.NodeParams) (*TestNode, error) {
	userAgent := "Hyperspace-Agent"
	password := "password"

	// Create server
	s, err := server.New(":0", userAgent, password, nodeParams)
	if err != nil {
		return nil, err
	}

	// Create client
	c := client.New(s.APIAddress())
	c.UserAgent = userAgent
	c.Password = password

	// Create TestNode
	tn := &TestNode{
		Server:      s,
		Client:      *c,
		params:      nodeParams,
		primarySeed: "",
	}
	if err = tn.initRootDirs(); err != nil {
		return nil, errors.AddContext(err, "failed to create root directories")
	}

	// Init wallet
	wip, err := tn.WalletInitPost("", false)
	if err != nil {
		return nil, err
	}
	tn.primarySeed = wip.PrimarySeed

	// Unlock wallet
	if err := tn.WalletUnlockPost(tn.primarySeed); err != nil {
		return nil, err
	}

	// Return TestNode
	return tn, nil
}

// initRootDirs creates the download and upload directories for the TestNode
func (tn *TestNode) initRootDirs() error {
	tn.downloadDir = &LocalDir{
		path: filepath.Join(tn.RenterDir(), "downloads"),
	}
	if err := os.MkdirAll(tn.downloadDir.path, 0777); err != nil {
		return err
	}
	tn.uploadDir = &LocalDir{
		path: filepath.Join(tn.RenterDir(), "uploads"),
	}
	if err := os.MkdirAll(tn.uploadDir.path, 0777); err != nil {
		return err
	}
	return nil
}

// HyperspacePath returns the hyperspacepath of a local file or directory to be used for
// uploading
func (tn *TestNode) HyperspacePath(path string) string {
	return strings.TrimPrefix(path, tn.RenterDir()+"/")
}
