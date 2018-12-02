// Package hostdb provides a HostDB object that implements the renter.hostDB
// interface. The blockchain is scanned for host announcements and hosts that
// are found get added to the host database. The database continually scans the
// set of hosts it has found and updates who is online.
package hostdb

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/hostdb/hosttree"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/threadgroup"

	"github.com/HyperspaceApp/errors"
)

var (
	// ErrInitialScanIncomplete is returned whenever an operation is not
	// allowed to be executed before the initial host scan has finished.
	ErrInitialScanIncomplete = errors.New("initial hostdb scan is not yet completed")
	errNilCS                 = errors.New("cannot create hostdb with nil consensus set")
	errNilGateway            = errors.New("cannot create hostdb with nil gateway")
)

// The HostDB is a database of potential hosts. It assigns a weight to each
// host based on their hosting parameters, and then can select hosts at random
// for uploading files.
type HostDB struct {
	// dependencies
	cs         modules.ConsensusSet
	deps       modules.Dependencies
	gateway    modules.Gateway
	log        *persist.Logger
	mu         sync.RWMutex
	persistDir string
	tg         threadgroup.ThreadGroup

	// The hostdb gets initialized with an allowance that can be modified. The
	// allowance is used to build a weightFunc that the hosttree depends on to
	// determine the weight of a host.
	allowance  modules.Allowance
	weightFunc hosttree.WeightFunc

	// The hostTree is the root node of the tree that organizes hosts by
	// weight. The tree is necessary for selecting weighted hosts at
	// random.
	hostTree *hosttree.HostTree

	// the scanPool is a set of hosts that need to be scanned. There are a
	// handful of goroutines constantly waiting on the channel for hosts to
	// scan. The scan map is used to prevent duplicates from entering the scan
	// pool.
	initialScanComplete     bool
	initialScanLatencies    []time.Duration
	disableIPViolationCheck bool
	scanList                []modules.HostDBEntry
	scanMap                 map[string]struct{}
	scanWait                bool
	scanningThreads         int

	// filteredTree is a hosttree that only contains the hosts that align with
	// the filterMode. The filteredHosts are the hosts that are submitted with
	// the filterMode to determine which host should be in the filteredTree
	filteredTree  *hosttree.HostTree
	filteredHosts map[string]types.SiaPublicKey
	filterMode    modules.FilterMode

	blockHeight types.BlockHeight
	lastChange  modules.ConsensusChangeID
}

// insert inserts the HostDBEntry into both hosttrees
func (hdb *HostDB) insert(host modules.HostDBEntry) error {
	err := hdb.hostTree.Insert(host)
	_, ok := hdb.filteredHosts[string(host.PublicKey.Key)]
	isWhitelist := hdb.filterMode == modules.HostDBActiveWhitelist
	if isWhitelist == ok {
		errF := hdb.filteredTree.Insert(host)
		if errF != nil && errF != hosttree.ErrHostExists {
			err = errors.Compose(err, errF)
		}
	}
	return err
}

// modify modifies the HostDBEntry in both hosttrees
func (hdb *HostDB) modify(host modules.HostDBEntry) error {
	err := hdb.hostTree.Modify(host)
	_, ok := hdb.filteredHosts[string(host.PublicKey.Key)]
	isWhitelist := hdb.filterMode == modules.HostDBActiveWhitelist
	if isWhitelist == ok {
		err = errors.Compose(err, hdb.filteredTree.Modify(host))
	}
	return err
}

// remove removes the HostDBEntry from both hosttrees
func (hdb *HostDB) remove(pk types.SiaPublicKey) error {
	err := hdb.hostTree.Remove(pk)
	_, ok := hdb.filteredHosts[string(pk.Key)]
	isWhitelist := hdb.filterMode == modules.HostDBActiveWhitelist
	if isWhitelist == ok {
		errF := hdb.filteredTree.Remove(pk)
		if err == nil && errF == hosttree.ErrNoSuchHost {
			return nil
		}
		err = errors.Compose(err, errF)
	}
	return err
}

// New returns a new HostDB.
func New(g modules.Gateway, cs modules.ConsensusSet, persistDir string) (*HostDB, error) {
	// Check for nil inputs.
	if g == nil {
		return nil, errNilGateway
	}
	if cs == nil {
		return nil, errNilCS
	}
	// Create HostDB using production dependencies.
	return NewCustomHostDB(g, cs, persistDir, modules.ProdDependencies)
}

// NewCustomHostDB creates a HostDB using the provided dependencies. It loads the old
// persistence data, spawns the HostDB's scanning threads, and subscribes it to
// the consensusSet.
func NewCustomHostDB(g modules.Gateway, cs modules.ConsensusSet, persistDir string, deps modules.Dependencies) (*HostDB, error) {
	// Create the HostDB object.
	hdb := &HostDB{
		cs:         cs,
		deps:       deps,
		gateway:    g,
		persistDir: persistDir,

		scanMap: make(map[string]struct{}),

		filteredHosts: make(map[string]types.SiaPublicKey),
	}

	// Set the hostweight function.
	hdb.allowance = modules.DefaultAllowance
	hdb.weightFunc = hdb.calculateHostWeightFn(hdb.allowance)

	// Create the persist directory if it does not yet exist.
	err := os.MkdirAll(persistDir, 0700)
	if err != nil {
		return nil, err
	}

	// Create the logger.
	logger, err := persist.NewFileLogger(filepath.Join(persistDir, "hostdb.log"))
	if err != nil {
		return nil, err
	}
	hdb.log = logger
	err = hdb.tg.AfterStop(func() error {
		if err := hdb.log.Close(); err != nil {
			// Resort to println as the logger is in an uncertain state.
			fmt.Println("Failed to close the hostdb logger:", err)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// The host tree is used to manage hosts and query them at random. The
	// filteredTree is used when whitelist or blacklist is enabled
	hdb.hostTree = hosttree.New(hdb.weightFunc, deps.Resolver())
	hdb.filteredTree = hdb.hostTree

	// Load the prior persistence structures.
	hdb.mu.Lock()
	err = hdb.load()
	hdb.mu.Unlock()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	err = hdb.tg.AfterStop(func() error {
		hdb.mu.Lock()
		err := hdb.saveSync()
		hdb.mu.Unlock()
		if err != nil {
			hdb.log.Println("Unable to save the hostdb:", err)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Loading is complete, establish the save loop.
	go hdb.threadedSaveLoop()

	// Don't perform the remaining startup in the presence of a quitAfterLoad
	// disruption.
	if hdb.deps.Disrupt("quitAfterLoad") {
		return hdb, nil
	}

	if hdb.cs.SpvMode() {
		err = cs.HeaderConsensusSetSubscribe(hdb, hdb.lastChange, hdb.tg.StopChan())
		if err == modules.ErrInvalidConsensusChangeID {
			// Subscribe again using the new ID. This will cause a triggered scan
			// on all of the hosts, but that should be acceptable.
			hdb.mu.Lock()
			hdb.blockHeight = 0
			hdb.lastChange = modules.ConsensusChangeBeginning
			hdb.mu.Unlock()
			err = cs.HeaderConsensusSetSubscribe(hdb, hdb.lastChange, hdb.tg.StopChan())
		}
		if err != nil {
			return nil, errors.New("hostdb header consensus subscription failed: " + err.Error())
		}
	} else {
		err = cs.ConsensusSetSubscribe(hdb, hdb.lastChange, hdb.tg.StopChan())
		if err == modules.ErrInvalidConsensusChangeID {
			// Subscribe again using the new ID. This will cause a triggered scan
			// on all of the hosts, but that should be acceptable.
			hdb.mu.Lock()
			hdb.blockHeight = 0
			hdb.lastChange = modules.ConsensusChangeBeginning
			hdb.mu.Unlock()
			err = cs.ConsensusSetSubscribe(hdb, hdb.lastChange, hdb.tg.StopChan())
		}
		if err != nil {
			return nil, errors.New("hostdb subscription failed: " + err.Error())
		}
	}
	err = hdb.tg.OnStop(func() error {
		cs.Unsubscribe(hdb)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Spawn the scan loop during production, but allow it to be disrupted
	// during testing. Primary reason is so that we can fill the hostdb with
	// fake hosts and not have them marked as offline as the scanloop operates.
	if !hdb.deps.Disrupt("disableScanLoop") {
		go hdb.threadedScan()
	} else {
		hdb.initialScanComplete = true
	}

	return hdb, nil
}

// ActiveHosts returns a list of hosts that are currently online, sorted by
// weight. If hostdb is in black or white list mode, then only active hosts from
// the filteredTree will be returned
func (hdb *HostDB) ActiveHosts() (activeHosts []modules.HostDBEntry) {
	allHosts := hdb.filteredTree.All()
	for _, entry := range allHosts {
		if len(entry.ScanHistory) == 0 {
			continue
		}
		if !entry.ScanHistory[len(entry.ScanHistory)-1].Success {
			continue
		}
		if !entry.AcceptingContracts {
			continue
		}
		activeHosts = append(activeHosts, entry)
	}
	return activeHosts
}

// AllHosts returns all of the hosts known to the hostdb, including the inactive
// ones. AllHosts is not filtered by blacklist or whitelist mode.
func (hdb *HostDB) AllHosts() (allHosts []modules.HostDBEntry) {
	return hdb.hostTree.All()
}

// CheckForIPViolations accepts a number of host public keys and returns the
// ones that violate the rules of the addressFilter.
func (hdb *HostDB) CheckForIPViolations(hosts []types.SiaPublicKey) []types.SiaPublicKey {
	// If the check was disabled we don't return any bad hosts.
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	disabled := hdb.disableIPViolationCheck
	if disabled {
		return nil
	}

	var entries []modules.HostDBEntry
	var badHosts []types.SiaPublicKey

	// Get the entries which correspond to the keys.
	for _, host := range hosts {
		entry, exists := hdb.hostTree.Select(host)
		if !exists {
			// A host that's not in the hostdb is bad.
			badHosts = append(badHosts, host)
			continue
		}
		entries = append(entries, entry)
	}

	// Sort the entries by the amount of time they have occupied their
	// corresponding subnets. This is the order in which they will be passed
	// into the filter which prioritizes entries which are passed in earlier.
	// That means 'younger' entries will be replaced in case of a violation.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastIPNetChange.Before(entries[j].LastIPNetChange)
	})

	// Create a filter and apply it.
	filter := hosttree.NewFilter(hdb.deps.Resolver())
	for _, entry := range entries {
		// Check if the host violates the rules.
		if filter.Filtered(entry.NetAddress) {
			badHosts = append(badHosts, entry.PublicKey)
			continue
		}
		// If it didn't then we add it to the filter.
		filter.Add(entry.NetAddress)
	}
	return badHosts
}

// Close closes the hostdb, terminating its scanning threads
func (hdb *HostDB) Close() error {
	return hdb.tg.Stop()
}

// Host returns the HostSettings associated with the specified pubkey. If no
// matching host is found, Host returns false.  For black and white list modes,
// the Filtered field for the HostDBEntry is set to indicate it the host is
// being filtered from the filtered hosttree
func (hdb *HostDB) Host(spk types.SiaPublicKey) (modules.HostDBEntry, bool) {
	hdb.mu.Lock()
	whitelist := hdb.filterMode == modules.HostDBActiveWhitelist
	filteredHosts := hdb.filteredHosts
	hdb.mu.Unlock()
	host, exists := hdb.hostTree.Select(spk)
	if !exists {
		return host, exists
	}
	_, ok := filteredHosts[string(spk.Key)]
	host.Filtered = whitelist != ok
	hdb.mu.RLock()
	updateHostHistoricInteractions(&host, hdb.blockHeight)
	hdb.mu.RUnlock()
	return host, exists
}

// Filter returns the hostdb's filterMode and filteredHosts
func (hdb *HostDB) Filter() (modules.FilterMode, map[string]types.SiaPublicKey) {
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	filteredHosts := make(map[string]types.SiaPublicKey)
	for k, v := range hdb.filteredHosts {
		filteredHosts[k] = v
	}
	return hdb.filterMode, filteredHosts
}

// SetFilterMode sets the hostdb filter mode
func (hdb *HostDB) SetFilterMode(fm modules.FilterMode, hosts []types.SiaPublicKey) error {
	hdb.mu.Lock()
	defer hdb.mu.Unlock()

	// Check for error
	if fm == modules.HostDBFilterError {
		return errors.New("Cannot set hostdb filter mode, provided filter mode is an error")
	}
	// Check if disabling
	if fm == modules.HostDBDisableFilter {
		hdb.filteredTree = hdb.hostTree
		hdb.filteredHosts = make(map[string]types.SiaPublicKey)
		hdb.filterMode = fm
		return nil
	}

	// Check for no hosts submitted with whitelist enabled
	isWhitelist := fm == modules.HostDBActiveWhitelist
	if len(hosts) == 0 && isWhitelist {
		return errors.New("cannot enable whitelist without hosts")
	}

	// Create filtered HostTree
	hdb.filteredTree = hosttree.New(hdb.weightFunc, modules.ProdDependencies.Resolver())

	// Create filteredHosts map
	filteredHosts := make(map[string]types.SiaPublicKey)
	for _, h := range hosts {
		if _, ok := filteredHosts[string(h.Key)]; ok {
			continue
		}
		filteredHosts[string(h.Key)] = h
	}
	var allErrs error
	allHosts := hdb.hostTree.All()
	for _, host := range allHosts {
		// Add hosts to filtered tree
		_, ok := filteredHosts[string(host.PublicKey.Key)]
		if isWhitelist != ok {
			continue
		}
		err := hdb.filteredTree.Insert(host)
		if err != nil {
			allErrs = errors.Compose(allErrs, err)
		}
	}
	hdb.filteredHosts = filteredHosts
	hdb.filterMode = fm
	return errors.Compose(allErrs, hdb.saveSync())
}

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (hdb *HostDB) InitialScanComplete() (complete bool, err error) {
	if err = hdb.tg.Add(); err != nil {
		return
	}
	defer hdb.tg.Done()
	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	complete = hdb.initialScanComplete
	return
}

// IPViolationsCheck returns a boolean indicating if the IP violation check is
// enabled or not.
func (hdb *HostDB) IPViolationsCheck() bool {
	hdb.mu.RLock()
	defer hdb.mu.RUnlock()
	return !hdb.disableIPViolationCheck
}

// RandomHosts implements the HostDB interface's RandomHosts() method. It takes
// a number of hosts to return, and a slice of netaddresses to ignore, and
// returns a slice of entries. If the IP violation check was disabled, the
// addressBlacklist is ignored.
func (hdb *HostDB) RandomHosts(n int, blacklist, addressBlacklist []types.SiaPublicKey) ([]modules.HostDBEntry, error) {
	hdb.mu.RLock()
	initialScanComplete := hdb.initialScanComplete
	ipCheckDisabled := hdb.disableIPViolationCheck
	hdb.mu.RUnlock()
	if !initialScanComplete {
		return []modules.HostDBEntry{}, ErrInitialScanIncomplete
	}
	if ipCheckDisabled {
		return hdb.filteredTree.SelectRandom(n, blacklist, nil), nil
	}
	return hdb.filteredTree.SelectRandom(n, blacklist, addressBlacklist), nil
}

// SetIPViolationCheck enables or disables the IP violation check. If disabled,
// CheckForIPViolations won't return bad hosts and RandomHosts will return the
// address blacklist.
func (hdb *HostDB) SetIPViolationCheck(enabled bool) {
	hdb.mu.Lock()
	defer hdb.mu.Unlock()
	hdb.disableIPViolationCheck = !enabled
}

// RandomHostsWithAllowance works as RandomHosts but uses a temporary hosttree
// created from the specified allowance. This is a very expensive call and
// should be used with caution.
func (hdb *HostDB) RandomHostsWithAllowance(n int, blacklist, addressBlacklist []types.SiaPublicKey, allowance modules.Allowance) ([]modules.HostDBEntry, error) {
	hdb.mu.RLock()
	initialScanComplete := hdb.initialScanComplete
	filteredHosts := hdb.filteredHosts
	filterType := hdb.filterMode
	hdb.mu.RUnlock()
	if !initialScanComplete {
		return []modules.HostDBEntry{}, ErrInitialScanIncomplete
	}
	// Create a temporary hosttree from the given allowance.
	ht := hosttree.New(hdb.calculateHostWeightFn(allowance), hdb.deps.Resolver())

	// Insert all known hosts.
	var insertErrs error
	allHosts := hdb.hostTree.All()
	isWhitelist := filterType == modules.HostDBActiveWhitelist
	for _, host := range allHosts {
		// Filter out listed hosts
		_, ok := filteredHosts[string(host.PublicKey.Key)]
		if isWhitelist != ok {
			continue
		}
		if err := ht.Insert(host); err != nil {
			insertErrs = errors.Compose(insertErrs, err)
		}
	}

	// Select hosts from the temporary hosttree.
	return ht.SelectRandom(n, blacklist, addressBlacklist), insertErrs
}

// SetAllowance updates the allowance used by the hostdb for weighing hosts by
// updating the host weight function. It will completely rebuild the hosttree so
// it should be used with care.
func (hdb *HostDB) SetAllowance(allowance modules.Allowance) error {
	// If the allowance is empty, set it to the default allowance. This ensures
	// that the estimates are at least moderately grounded.
	if reflect.DeepEqual(allowance, modules.Allowance{}) {
		allowance = modules.DefaultAllowance
	}

	// Update the weight function.
	hdb.mu.Lock()
	hdb.allowance = allowance
	hdb.weightFunc = hdb.calculateHostWeightFn(allowance)
	hdb.mu.Unlock()

	// Update the trees weight function.
	err1 := hdb.hostTree.SetWeightFunction(hdb.calculateHostWeightFn(allowance))
	err2 := hdb.filteredTree.SetWeightFunction(hdb.calculateHostWeightFn(allowance))
	return errors.Compose(err1, err2)
}
