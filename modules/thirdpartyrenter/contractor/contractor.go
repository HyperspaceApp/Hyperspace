package contractor

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/hostdb"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/hostdb/hosttree"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/proto"
	"github.com/HyperspaceApp/Hyperspace/node/api/client"
	"github.com/HyperspaceApp/Hyperspace/persist"
	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// Contractor is the contractor module for third party renter
type Contractor struct {
	// dependencies
	hdb        hostDB
	log        *persist.Logger
	mu         sync.RWMutex
	persist    persister
	staticDeps modules.Dependencies
	tg         siasync.ThreadGroup

	downloaders map[types.FileContractID]*hostDownloader
	editors     map[types.FileContractID]*hostEditor
	httpClient  *client.Client
	sessions    map[types.FileContractID]*hostSession
	// 	numFailedRenews     map[types.FileContractID]types.BlockHeight
	pubKeysToContractID map[string]types.FileContractID
	contractIDToPubKey  map[types.FileContractID]types.SiaPublicKey
	// 	renewing            map[types.FileContractID]bool // prevent revising during renewal

	blockHeight types.BlockHeight

	// 	// renewedFrom links the new contract's ID to the old contract's ID
	// 	// renewedTo links the old contract's ID to the new contract's ID
	staticContracts *proto.ContractSet
	// 	oldContracts         map[types.FileContractID]modules.RenterContract
	// 	recoverableContracts []modules.RecoverableContract
	// 	renewedFrom          map[types.FileContractID]types.FileContractID
	// 	renewedTo            map[types.FileContractID]types.FileContractID
}

// New returns a new Contractor.
func New(persistDir string) (*Contractor, error) {
	// Create the persist directory if it does not yet exist.
	if err := os.MkdirAll(persistDir, 0700); err != nil {
		return nil, err
	}
	// Create the contract set.
	contractSet, err := proto.NewContractSet(filepath.Join(persistDir, "contracts"), modules.ProdDependencies)
	if err != nil {
		return nil, err
	}
	// Create the logger.
	logger, err := persist.NewFileLogger(filepath.Join(persistDir, "contractor.log"))
	if err != nil {
		return nil, err
	}

	// Create Contractor using production dependencies.
	return NewCustomContractor(contractSet, NewPersist(persistDir), logger, modules.ProdDependencies)
}

// NewCustomContractor creates a Contractor using the provided dependencies.
func NewCustomContractor(contractSet *proto.ContractSet, p persister, l *persist.Logger, deps modules.Dependencies) (*Contractor, error) {
	hdb, _ := hostdb.NewEmptyHostDB()
	// Create the Contractor object.
	c := &Contractor{
		staticDeps: deps,
		hdb:        hdb,
		log:        l,
		persist:    p,
		httpClient: client.New("localhost:5585"),

		staticContracts: contractSet,
		downloaders:     make(map[types.FileContractID]*hostDownloader),
		editors:         make(map[types.FileContractID]*hostEditor),
		sessions:        make(map[types.FileContractID]*hostSession),
		// oldContracts:        make(map[types.FileContractID]modules.RenterContract),
		contractIDToPubKey:  make(map[types.FileContractID]types.SiaPublicKey),
		pubKeysToContractID: make(map[string]types.FileContractID),
	}

	// Close the contract set and logger upon shutdown.
	c.tg.AfterStop(func() {
		// 	if err := c.staticContracts.Close(); err != nil {
		// 		c.log.Println("Failed to close contract set:", err)
		// 	}
		if err := c.log.Close(); err != nil {
			fmt.Println("Failed to close the contractor logger:", err)
		}
	})

	// Load the prior persistence structures.
	// err := c.load()
	// if err != nil && !os.IsNotExist(err) {
	// 	return nil, err
	// }

	// We may have upgraded persist or resubscribed. Save now so that we don't
	// lose our work.
	// c.mu.Lock()
	// err = c.save()
	// c.mu.Unlock()
	// if err != nil {
	// 	return nil, err
	// }

	// // Initialize the contractIDToPubKey map
	// for _, contract := range c.oldContracts {
	// 	c.contractIDToPubKey[contract.ID] = contract.HostPublicKey
	// 	c.pubKeysToContractID[string(contract.HostPublicKey.Key)] = contract.ID
	// }
	// for _, contract := range c.staticContracts.ViewAll() {
	// 	c.contractIDToPubKey[contract.ID] = contract.HostPublicKey
	// 	c.pubKeysToContractID[string(contract.HostPublicKey.Key)] = contract.ID
	// }

	// Update the allowance in the hostdb with the one that was loaded from
	// disk.
	return c, nil
}

// Close closes the Contractor.
func (c *Contractor) Close() error {
	return c.tg.Stop()
}

// ContractByPublicKey returns the contract with the key specified, if it
// exists. The contract will be resolved if possible to the most recent child
// contract.
func (c *Contractor) ContractByPublicKey(pk types.SiaPublicKey) (modules.RenterContract, bool) {
	// TODO: fetch from server
	return modules.RenterContract{}, false
}

// ContractUtility this is a mock
func (c *Contractor) ContractUtility(types.SiaPublicKey) (modules.ContractUtility, bool) {
	return modules.ContractUtility{
		true,
		true,
		false,
	}, true
}

// Contracts fetch contracts from remote
func (c *Contractor) Contracts() []modules.RenterContract {
	// fetch our contracts from remote server
	// TODO: add auth and our id
	log.Printf("1")
	trc, err := c.httpClient.ThirdpartyServerContractsGet()
	if err != nil {
		log.Println("Could not get contracts:", err)
		return nil
	}
	log.Printf("2")
	c.blockHeight = trc.Height
	// var contracts []modules.RenterContract
	for _, remoteContract := range trc.Contracts {
		// TODO: add hash if has one
		c.staticContracts.ThirdpartyInsertORUpdateContract(remoteContract)

		c.contractIDToPubKey[remoteContract.ID] = remoteContract.HostPublicKey
		c.pubKeysToContractID[string(remoteContract.HostPublicKey.Key)] = remoteContract.ID
	}
	log.Printf("3")

	// sync hostdb after sync contracts
	hdag, err := c.httpClient.ThirdpartyServerHostDbAllGet()
	if err != nil {
		log.Println("Could not get hosts:", err)
	}
	for _, extendedHost := range hdag.Hosts {
		err := c.hdb.ThirdpartyInsert(extendedHost.HostDBEntry)
		if err != nil && err != hosttree.ErrHostExists {
			c.log.Println("Could not insert host:", err)
		}
	}

	return c.staticContracts.ViewAll()
}

// IsOffline reports whether the specified host is considered offline.
func (c *Contractor) IsOffline(types.SiaPublicKey) bool {
	return false
}

// ResolveIDToPubKey returns the ID of the most recent renewal of id.
func (c *Contractor) ResolveIDToPubKey(id types.FileContractID) types.SiaPublicKey {
	// c.mu.RLock()
	// defer c.mu.RUnlock()
	// pk, exists := c.contractIDToPubKey[id]
	// if !exists {
	// 	panic("renewed should never miss an id")
	// }
	contracts := c.Contracts()
	for _, c := range contracts {
		if c.ID == id {
			return c.HostPublicKey
		}
	}
	return types.SiaPublicKey{}
}
