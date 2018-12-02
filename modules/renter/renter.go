// Package renter is responsible for uploading and downloading files on the sia
// network.
package renter

// NOTE: Some of the concurrency patterns in the renter are fairly complex. A
// lot of this has been cleaned up, though some shadows of previous demons still
// remain. Be careful when working with anything that involves concurrency.

// TODO: Allow the 'baseMemory' to be set by the user.

// TODO: The repair loop currently receives new upload jobs through a channel.
// The download loop has a better model, a heap that can be pushed to and popped
// from concurrently without needing complex channel communication. Migrating
// the renter to this model should clean up some of the places where uploading
// bottlenecks, and reduce the amount of channel-ninjitsu required to make the
// uploading function.

// TODO: Allow user to configure the packet size when ratelimiting the renter.
// Currently the default is set to 16kb. That's going to require updating the
// API and extending the settings object, and then tweaking the
// setBandwidthLimits function.

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/contractor"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/hostdb"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siafile"
	"github.com/HyperspaceApp/Hyperspace/persist"
	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/HyperspaceApp/errors"
	"github.com/HyperspaceApp/threadgroup"
	"github.com/HyperspaceApp/writeaheadlog"
)

var (
	errNilContractor = errors.New("cannot create renter with nil contractor")
	errNilCS         = errors.New("cannot create renter with nil consensus set")
	errNilGateway    = errors.New("cannot create hostdb with nil gateway")
	errNilHdb        = errors.New("cannot create renter with nil hostdb")
	errNilTpool      = errors.New("cannot create renter with nil transaction pool")
)

// A hostDB is a database of hosts that the renter can use for figuring out who
// to upload to, and download from.
type hostDB interface {
	// ActiveHosts returns the list of hosts that are actively being selected
	// from.
	ActiveHosts() []modules.HostDBEntry

	// AllHosts returns the full list of hosts known to the hostdb, sorted in
	// order of preference.
	AllHosts() []modules.HostDBEntry

	// Close closes the hostdb.
	Close() error

	// Filter returns the hostdb's filterMode and filteredHosts
	Filter() (modules.FilterMode, map[string]types.SiaPublicKey)

	// SetFilterMode sets the renter's hostdb filter mode
	SetFilterMode(lm modules.FilterMode, hosts []types.SiaPublicKey) error

	// Host returns the HostDBEntry for a given host.
	Host(pk types.SiaPublicKey) (modules.HostDBEntry, bool)

	// initialScanComplete returns a boolean indicating if the initial scan of the
	// hostdb is completed.
	InitialScanComplete() (bool, error)

	// IPViolationsCheck returns a boolean indicating if the IP violation check is
	// enabled or not.
	IPViolationsCheck() bool

	// RandomHosts returns a set of random hosts, weighted by their estimated
	// usefulness / attractiveness to the renter. RandomHosts will not return
	// any offline or inactive hosts.
	RandomHosts(int, []types.SiaPublicKey, []types.SiaPublicKey) ([]modules.HostDBEntry, error)

	// RandomHostsWithAllowance is the same as RandomHosts but accepts an
	// allowance as an argument to be used instead of the allowance set in the
	// renter.
	RandomHostsWithAllowance(int, []types.SiaPublicKey, []types.SiaPublicKey, modules.Allowance) ([]modules.HostDBEntry, error)

	// ScoreBreakdown returns a detailed explanation of the various properties
	// of the host.
	ScoreBreakdown(modules.HostDBEntry) modules.HostScoreBreakdown

	// SetIPViolationCheck enables/disables the IP violation check within the
	// hostdb.
	SetIPViolationCheck(enabled bool)

	// EstimateHostScore returns the estimated score breakdown of a host with the
	// provided settings.
	EstimateHostScore(modules.HostDBEntry, modules.Allowance) modules.HostScoreBreakdown
}

// A hostContractor negotiates, revises, renews, and provides access to file
// contracts.
type hostContractor interface {
	// SetAllowance sets the amount of money the contractor is allowed to
	// spend on contracts over a given time period, divided among the number
	// of hosts specified. Note that contractor can start forming contracts as
	// soon as SetAllowance is called; that is, it may block.
	SetAllowance(modules.Allowance) error

	// Allowance returns the current allowance
	Allowance() modules.Allowance

	// Close closes the hostContractor.
	Close() error

	// CancelContract cancels the Renter's contract
	CancelContract(id types.FileContractID) error

	// Contracts returns the staticContracts of the renter's hostContractor.
	Contracts() []modules.RenterContract

	// OldContracts returns the oldContracts of the renter's hostContractor.
	OldContracts() []modules.RenterContract

	// ContractByPublicKey returns the contract associated with the host key.
	ContractByPublicKey(types.SiaPublicKey) (modules.RenterContract, bool)

	// ContractUtility returns the utility field for a given contract, along
	// with a bool indicating if it exists.
	ContractUtility(types.SiaPublicKey) (modules.ContractUtility, bool)

	// CurrentPeriod returns the height at which the current allowance period
	// began.
	CurrentPeriod() types.BlockHeight

	// PeriodSpending returns the amount spent on contracts during the current
	// billing period.
	PeriodSpending() modules.ContractorSpending

	// Editor creates an Editor from the specified contract ID, allowing the
	// insertion, deletion, and modification of sectors.
	Editor(types.SiaPublicKey, <-chan struct{}) (contractor.Editor, error)

	// IsOffline reports whether the specified host is considered offline.
	IsOffline(types.SiaPublicKey) bool

	// Downloader creates a Downloader from the specified contract ID,
	// allowing the retrieval of sectors.
	Downloader(types.SiaPublicKey, <-chan struct{}) (contractor.Downloader, error)

	// ResolveIDToPubKey returns the public key of a host given a contract id.
	ResolveIDToPubKey(types.FileContractID) types.SiaPublicKey

	// RateLimits Gets the bandwidth limits for connections created by the
	// contractor and its submodules.
	RateLimits() (readBPS int64, writeBPS int64, packetSize uint64)

	// SetRateLimits sets the bandwidth limits for connections created by the
	// contractor and its submodules.
	SetRateLimits(int64, int64, uint64)
}

// A Renter is responsible for tracking all of the files that a user has
// uploaded to Sia, as well as the locations and health of these files.
//
// TODO: Separate the workerPool to have its own mutex. The workerPool doesn't
// interfere with any of the other fields in the renter, should be fine for it
// to have a separate mutex, that way operations on the worker pool don't block
// operations on other parts of the struct. If we're going to do it that way,
// might make sense to split the worker pool off into it's own struct entirely
// the same way that we split of the memoryManager entirely.
type Renter struct {
	// File management.
	//
	files map[string]*siafile.SiaFile

	// Download management. The heap has a separate mutex because it is always
	// accessed in isolation.
	downloadHeapMu sync.Mutex         // Used to protect the downloadHeap.
	downloadHeap   *downloadChunkHeap // A heap of priority-sorted chunks to download.
	newDownloads   chan struct{}      // Used to notify download loop that new downloads are available.

	// Download history. The history list has its own mutex because it is always
	// accessed in isolation.
	//
	// TODO: Currently the download history doesn't include repair-initiated
	// downloads, and instead only contains user-initiated downloads.
	downloadHistory   []*download
	downloadHistoryMu sync.Mutex

	// Upload management.
	uploadHeap uploadHeap

	// List of workers that can be used for uploading and/or downloading.
	memoryManager *memoryManager
	workerPool    map[types.FileContractID]*worker

	// Cache the hosts from the last price estimation result.
	lastEstimationHosts []modules.HostDBEntry

	// Utilities.
	staticStreamCache *streamCache
	cs                modules.ConsensusSet
	deps              modules.Dependencies
	g                 modules.Gateway
	hostContractor    hostContractor
	hostDB            hostDB
	log               *persist.Logger
	persist           persistence
	persistDir        string
	mu                *siasync.RWMutex
	tg                threadgroup.ThreadGroup
	tpool             modules.TransactionPool
	wal               *writeaheadlog.WAL
}

// Close closes the Renter and its dependencies
func (r *Renter) Close() error {
	r.tg.Stop()
	r.hostDB.Close()
	return r.hostContractor.Close()
}

// PriceEstimation estimates the cost in siacoins of performing various storage
// and data operations.  The estimation will be done using the provided
// allowance, if an empty allowance is provided then the renter's current
// allowance will be used if one is set.  The final allowance used will be
// returned.
func (r *Renter) PriceEstimation(allowance modules.Allowance) (modules.RenterPriceEstimation, modules.Allowance, error) {
	// Use provide allowance. If no allowance provided use the existing
	// allowance. If no allowance exists, use a sane default allowance.
	if reflect.DeepEqual(allowance, modules.Allowance{}) {
		rs := r.Settings()
		allowance = rs.Allowance
		if reflect.DeepEqual(allowance, modules.Allowance{}) {
			allowance = modules.DefaultAllowance
		}
	}

	// Get hosts for estimate
	var hosts []modules.HostDBEntry
	hostmap := make(map[string]struct{})

	// Start by grabbing hosts from contracts
	// Get host pubkeys from contracts
	contracts := r.Contracts()
	var pks []types.SiaPublicKey
	for _, c := range contracts {
		u, ok := r.ContractUtility(c.HostPublicKey)
		if !ok {
			continue
		}
		// Check for active contracts only
		if !u.GoodForRenew {
			continue
		}
		pks = append(pks, c.HostPublicKey)
	}
	// Get hosts from pubkeys
	for _, pk := range pks {
		host, ok := r.hostDB.Host(pk)
		if !ok || host.Filtered {
			continue
		}
		// confirm host wasn't already added
		if _, ok := hostmap[host.PublicKey.String()]; ok {
			continue
		}
		hosts = append(hosts, host)
		hostmap[host.PublicKey.String()] = struct{}{}
	}
	// Add hosts from previous estimate cache if needed
	if len(hosts) < int(allowance.Hosts) {
		id := r.mu.Lock()
		cachedHosts := r.lastEstimationHosts
		r.mu.Unlock(id)
		for _, host := range cachedHosts {
			// confirm host wasn't already added
			if _, ok := hostmap[host.PublicKey.String()]; ok {
				continue
			}
			hosts = append(hosts, host)
			hostmap[host.PublicKey.String()] = struct{}{}
		}
	}
	// Add random hosts if needed
	if len(hosts) < int(allowance.Hosts) {
		// Re-initialize the list with SiaPublicKeys to hold the public keys from the current
		// set of hosts. This list will be used as address filter when requesting random hosts.
		var pks []types.SiaPublicKey
		for _, host := range hosts {
			pks = append(pks, host.PublicKey)
		}
		// Grab hosts to perform the estimation.
		var err error
		randHosts, err := r.hostDB.RandomHostsWithAllowance(int(allowance.Hosts)-len(hosts), pks, pks, allowance)
		if err != nil {
			return modules.RenterPriceEstimation{}, allowance, errors.AddContext(err, "could not generate estimate, could not get random hosts")
		}
		// As the returned random hosts are checked for IP violations and double entries against the current
		// slice of hosts, the returned hosts can be safely added to the current slice.
		hosts = append(hosts, randHosts...)
	}
	// Check if there are zero hosts, which means no estimation can be made.
	if len(hosts) == 0 {
		return modules.RenterPriceEstimation{}, allowance, errors.New("estimate cannot be made, there are no hosts")
	}

	// Add up the costs for each host.
	var totalContractCost types.Currency
	var totalDownloadCost types.Currency
	var totalStorageCost types.Currency
	var totalUploadCost types.Currency
	for _, host := range hosts {
		totalContractCost = totalContractCost.Add(host.ContractPrice)
		totalDownloadCost = totalDownloadCost.Add(host.DownloadBandwidthPrice)
		totalStorageCost = totalStorageCost.Add(host.StoragePrice)
		totalUploadCost = totalUploadCost.Add(host.UploadBandwidthPrice)
	}

	// Convert values to being human-scale.
	totalDownloadCost = totalDownloadCost.Mul(modules.BytesPerTerabyte)
	totalStorageCost = totalStorageCost.Mul(modules.BlockBytesPerMonthTerabyte)
	totalUploadCost = totalUploadCost.Mul(modules.BytesPerTerabyte)

	// Factor in redundancy.
	totalStorageCost = totalStorageCost.Mul64(3) // TODO: follow file settings?
	totalUploadCost = totalUploadCost.Mul64(3)   // TODO: follow file settings?

	// Perform averages.
	totalContractCost = totalContractCost.Div64(uint64(len(hosts)))
	totalDownloadCost = totalDownloadCost.Div64(uint64(len(hosts)))
	totalStorageCost = totalStorageCost.Div64(uint64(len(hosts)))
	totalUploadCost = totalUploadCost.Div64(uint64(len(hosts)))

	// Take the average of the host set to estimate the overall cost of the
	// contract forming. This is to protect against the case where less hosts
	// were gathered for the estimate that the allowance requires
	totalContractCost = totalContractCost.Mul64(allowance.Hosts)

	// Add the cost of paying the transaction fees and then double the contract
	// costs to account for renewing a full set of contracts.
	_, feePerByte := r.tpool.FeeEstimation()
	txnsFees := feePerByte.Mul64(modules.EstimatedFileContractTransactionSetSize).Mul64(uint64(allowance.Hosts))
	totalContractCost = totalContractCost.Add(txnsFees)
	totalContractCost = totalContractCost.Mul64(2)

	// Determine host collateral to be added to siafund fee
	var hostCollateral types.Currency
	contractCostPerHost := totalContractCost.Div64(allowance.Hosts)
	fundingPerHost := allowance.Funds.Div64(allowance.Hosts)
	numHosts := uint64(0)
	for _, host := range hosts {
		// Assume that the ContractPrice equals contractCostPerHost and that
		// the txnFee was zero. It doesn't matter since RenterPayoutsPreTax
		// simply subtracts both values from the funding.
		host.ContractPrice = contractCostPerHost
		expectedStorage := allowance.ExpectedStorage / uint64(len(hosts))
		_, _, collateral, err := modules.RenterPayouts(host, fundingPerHost, types.ZeroCurrency, types.ZeroCurrency, types.ZeroCurrency, allowance.Period, expectedStorage)
		if err != nil {
			continue
		}
		hostCollateral = hostCollateral.Add(collateral)
		numHosts++
	}

	// Divide by zero check. The only way to get 0 numHosts is if
	// RenterPayoutsPreTax errors for every host. This would happend if the
	// funding of the allowance is not enough as that would cause the
	// fundingPerHost to be less than the contract price
	if numHosts == 0 {
		return modules.RenterPriceEstimation{}, allowance, errors.New("funding insufficient for number of hosts")
	}
	// Calculate average collateral and determine collateral for allowance
	hostCollateral = hostCollateral.Div64(numHosts)
	hostCollateral = hostCollateral.Mul64(allowance.Hosts)

	// Increase estimates by a factor of safety to account for host churn and
	// any potential missed additions
	totalContractCost = totalContractCost.MulFloat(PriceEstimationSafetyFactor)
	totalDownloadCost = totalDownloadCost.MulFloat(PriceEstimationSafetyFactor)
	totalStorageCost = totalStorageCost.MulFloat(PriceEstimationSafetyFactor)
	totalUploadCost = totalUploadCost.MulFloat(PriceEstimationSafetyFactor)

	est := modules.RenterPriceEstimation{
		FormContracts:        totalContractCost,
		DownloadTerabyte:     totalDownloadCost,
		StorageTerabyteMonth: totalStorageCost,
		UploadTerabyte:       totalUploadCost,
	}

	id := r.mu.Lock()
	r.lastEstimationHosts = hosts
	r.mu.Unlock(id)

	return est, allowance, nil
}

// setBandwidthLimits will change the bandwidth limits of the renter based on
// the persist values for the bandwidth.
func (r *Renter) setBandwidthLimits(downloadSpeed int64, uploadSpeed int64) error {
	// Input validation.
	if downloadSpeed < 0 || uploadSpeed < 0 {
		return errors.New("download/upload rate limit can't be below 0")
	}

	// Check for sentinel "no limits" value.
	if downloadSpeed == 0 && uploadSpeed == 0 {
		r.hostContractor.SetRateLimits(0, 0, 0)
	} else {
		// Set the rate limits according to the provided values.
		r.hostContractor.SetRateLimits(downloadSpeed, uploadSpeed, 4*4096)
	}
	return nil
}

// SetSettings will update the settings for the renter.
//
// NOTE: This function can't be atomic. Typically we try to have user requests
// be atomic, so that either everything changes or nothing changes, but since
// these changes happen progressively, it's possible for some of the settings
// (like the allowance) to succeed, but then if the bandwidth limits for example
// are bad, then the allowance will update but the bandwidth will not update.
func (r *Renter) SetSettings(s modules.RenterSettings) error {
	// Early input validation.
	if s.MaxDownloadSpeed < 0 || s.MaxUploadSpeed < 0 {
		return errors.New("bandwidth limits cannot be negative")
	}
	if s.StreamCacheSize <= 0 {
		return errors.New("stream cache size needs to be 1 or larger")
	}

	// Set allowance.
	err := r.hostContractor.SetAllowance(s.Allowance)
	if err != nil {
		return err
	}

	// Set the bandwidth limits.
	err = r.setBandwidthLimits(s.MaxDownloadSpeed, s.MaxUploadSpeed)
	if err != nil {
		return err
	}
	r.persist.MaxDownloadSpeed = s.MaxDownloadSpeed
	r.persist.MaxUploadSpeed = s.MaxUploadSpeed

	// Set StreamingCacheSize
	err = r.staticStreamCache.SetStreamingCacheSize(s.StreamCacheSize)
	if err != nil {
		return err
	}
	r.persist.StreamCacheSize = s.StreamCacheSize

	// Set IPViolationsCheck
	r.hostDB.SetIPViolationCheck(s.IPViolationsCheck)

	// Save the changes.
	err = r.saveSync()
	if err != nil {
		return err
	}

	// Update the worker pool so that the changes are immediately apparent to
	// users.
	r.managedUpdateWorkerPool()
	return nil
}

// SetFileTrackingPath sets the on-disk location of an uploaded file to a new
// value. Useful if files need to be moved on disk. SetFileTrackingPath will
// check that a file exists at the new location and it ensures that it has the
// right size, but it can't check that the content is the same. Therefore the
// caller is responsible for not accidentally corrupting the uploaded file by
// providing a different file with the same size.
func (r *Renter) SetFileTrackingPath(siaPath, newPath string) error {
	id := r.mu.Lock()

	// Check if file exists and is being tracked.
	file, exists := r.files[siaPath]
	if !exists {
		r.mu.Unlock(id)
		return fmt.Errorf("unknown file %s", siaPath)
	}

	// Sanity check that a file with the correct size exists at the new
	// location.
	fi, err := os.Stat(newPath)
	if err != nil {
		r.mu.Unlock(id)
		return errors.AddContext(err, "failed to get fileinfo of the file")
	}
	if uint64(fi.Size()) != file.Size() {
		r.mu.Unlock(id)
		return fmt.Errorf("file sizes don't match - want %v but got %v", file.Size(), fi.Size())
	}

	r.mu.Unlock(id)

	// Set the new path on disk.
	return file.SetLocalPath(newPath)
}

// ActiveHosts returns an array of hostDB's active hosts
func (r *Renter) ActiveHosts() []modules.HostDBEntry { return r.hostDB.ActiveHosts() }

// AllHosts returns an array of all hosts
func (r *Renter) AllHosts() []modules.HostDBEntry { return r.hostDB.AllHosts() }

// SetFilterMode sets the renter's hostdb filter mode
func (r *Renter) SetFilterMode(lm modules.FilterMode, hosts []types.SiaPublicKey) error {
	// Check to see how many hosts are needed for the allowance
	minHosts := r.Settings().Allowance.Hosts
	if len(hosts) < int(minHosts) && lm == modules.HostDBActiveWhitelist {
		r.log.Printf("WARN: There are fewer whitelisted hosts than the allowance requires.  Have %v whitelisted hosts, need %v to support allowance\n", len(hosts), minHosts)
	}

	// Set list mode filter for the hostdb
	if err := r.hostDB.SetFilterMode(lm, hosts); err != nil {
		return err
	}

	return nil
}

// Host returns the host associated with the given public key
func (r *Renter) Host(spk types.SiaPublicKey) (modules.HostDBEntry, bool) { return r.hostDB.Host(spk) }

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (r *Renter) InitialScanComplete() (bool, error) { return r.hostDB.InitialScanComplete() }

// ScoreBreakdown returns the score breakdown
func (r *Renter) ScoreBreakdown(e modules.HostDBEntry) modules.HostScoreBreakdown {
	return r.hostDB.ScoreBreakdown(e)
}

// EstimateHostScore returns the estimated host score
func (r *Renter) EstimateHostScore(e modules.HostDBEntry, a modules.Allowance) modules.HostScoreBreakdown {
	if reflect.DeepEqual(a, modules.Allowance{}) {
		a = r.Settings().Allowance
	}
	if reflect.DeepEqual(a, modules.Allowance{}) {
		a = modules.DefaultAllowance
	}
	return r.hostDB.EstimateHostScore(e, a)
}

// CancelContract cancels a renter's contract by ID by setting goodForRenew and goodForUpload to false
func (r *Renter) CancelContract(id types.FileContractID) error {
	return r.hostContractor.CancelContract(id)
}

// Contracts returns an array of host contractor's staticContracts
func (r *Renter) Contracts() []modules.RenterContract { return r.hostContractor.Contracts() }

// OldContracts returns an array of host contractor's oldContracts
func (r *Renter) OldContracts() []modules.RenterContract {
	return r.hostContractor.OldContracts()
}

// CurrentPeriod returns the host contractor's current period
func (r *Renter) CurrentPeriod() types.BlockHeight { return r.hostContractor.CurrentPeriod() }

// ContractUtility returns the utility field for a given contract, along
// with a bool indicating if it exists.
func (r *Renter) ContractUtility(pk types.SiaPublicKey) (modules.ContractUtility, bool) {
	return r.hostContractor.ContractUtility(pk)
}

// PeriodSpending returns the host contractor's period spending
func (r *Renter) PeriodSpending() modules.ContractorSpending { return r.hostContractor.PeriodSpending() }

// Settings returns the renter's allowance
func (r *Renter) Settings() modules.RenterSettings {
	download, upload, _ := r.hostContractor.RateLimits()
	return modules.RenterSettings{
		Allowance:         r.hostContractor.Allowance(),
		IPViolationsCheck: r.hostDB.IPViolationsCheck(),
		MaxDownloadSpeed:  download,
		MaxUploadSpeed:    upload,
		StreamCacheSize:   r.staticStreamCache.cacheSize,
	}
}

// ProcessConsensusChange returns the process consensus change
func (r *Renter) ProcessConsensusChange(cc modules.ConsensusChange) {
	id := r.mu.Lock()
	r.lastEstimationHosts = []modules.HostDBEntry{}
	r.mu.Unlock(id)
}

// ProcessHeaderConsensusChange will reset the lastEstimation
func (r *Renter) ProcessHeaderConsensusChange(hcc modules.HeaderConsensusChange) {
	id := r.mu.Lock()
	r.lastEstimationHosts = []modules.HostDBEntry{}
	r.mu.Unlock(id)
}

// SetIPViolationCheck is a passthrough method to the hostdb's method of the
// same name.
func (r *Renter) SetIPViolationCheck(enabled bool) {
	r.hostDB.SetIPViolationCheck(enabled)
}

// validateSiapath checks that a Siapath is a legal filename.
// ../ is disallowed to prevent directory traversal, and paths must not begin
// with / or be empty.
func validateSiapath(hyperspacepath string) error {
	if hyperspacepath == "" {
		return ErrEmptyFilename
	}
	if hyperspacepath == ".." {
		return errors.New("hyperspacepath cannot be '..'")
	}
	if hyperspacepath == "." {
		return errors.New("hyperspacepath cannot be '.'")
	}
	// check prefix
	if strings.HasPrefix(hyperspacepath, "/") {
		return errors.New("hyperspacepath cannot begin with /")
	}
	if strings.HasPrefix(hyperspacepath, "../") {
		return errors.New("hyperspacepath cannot begin with ../")
	}
	if strings.HasPrefix(hyperspacepath, "./") {
		return errors.New("hyperspacepath connot begin with ./")
	}
	var prevElem string
	for _, pathElem := range strings.Split(hyperspacepath, "/") {
		if pathElem == "." || pathElem == ".." {
			return errors.New("hyperspacepath cannot contain . or .. elements")
		}
		if prevElem != "" && pathElem == "" {
			return ErrEmptyFilename
		}
		prevElem = pathElem
	}
	return nil
}

// Enforce that Renter satisfies the modules.Renter interface.
var _ modules.Renter = (*Renter)(nil)

// NewCustomRenter initializes a renter and returns it.
func NewCustomRenter(g modules.Gateway, cs modules.ConsensusSet, tpool modules.TransactionPool, hdb hostDB, hc hostContractor, persistDir string, deps modules.Dependencies) (*Renter, error) {
	if g == nil {
		return nil, errNilGateway
	}
	if cs == nil {
		return nil, errNilCS
	}
	if tpool == nil {
		return nil, errNilTpool
	}
	if hc == nil {
		return nil, errNilContractor
	}
	if hdb == nil && build.Release != "testing" {
		return nil, errNilHdb
	}

	r := &Renter{
		files: make(map[string]*siafile.SiaFile),

		// Making newDownloads a buffered channel means that most of the time, a
		// new download will trigger an unnecessary extra iteration of the
		// download heap loop, searching for a chunk that's not there. This is
		// preferable to the alternative, where in rare cases the download heap
		// will miss work altogether.
		newDownloads: make(chan struct{}, 1),
		downloadHeap: new(downloadChunkHeap),

		uploadHeap: uploadHeap{
			activeChunks: make(map[uploadChunkID]struct{}),
			newUploads:   make(chan struct{}, 1),
		},

		workerPool: make(map[types.FileContractID]*worker),

		cs:             cs,
		deps:           deps,
		g:              g,
		hostDB:         hdb,
		hostContractor: hc,
		persistDir:     persistDir,
		mu:             siasync.New(modules.SafeMutexDelay, 1),
		tpool:          tpool,
	}
	r.memoryManager = newMemoryManager(defaultMemory, r.tg.StopChan())

	// Load all saved data.
	if err := r.initPersist(); err != nil {
		return nil, err
	}

	// Set the bandwidth limits, since the contractor doesn't persist them.
	//
	// TODO: Reconsider the way that the bandwidth limits are allocated to the
	// renter module, because really it seems they only impact the contractor.
	// The renter itself doesn't actually do any uploading or downloading.
	err := r.setBandwidthLimits(r.persist.MaxDownloadSpeed, r.persist.MaxUploadSpeed)
	if err != nil {
		return nil, err
	}

	// Initialize the streaming cache.
	r.staticStreamCache = newStreamCache(r.persist.StreamCacheSize)

	if cs.SpvMode() {
		// Subscribe to the consensus set.
		err = cs.HeaderConsensusSetSubscribe(r, modules.ConsensusChangeRecent, r.tg.StopChan())
		if err != nil {
			return nil, err
		}
	} else {
		// Subscribe to the consensus set.
		err = cs.ConsensusSetSubscribe(r, modules.ConsensusChangeRecent, r.tg.StopChan())
		if err != nil {
			return nil, err
		}
	}

	// Spin up the workers for the work pool.
	r.managedUpdateWorkerPool()
	go r.threadedDownloadLoop()
	go r.threadedUploadLoop()

	// Kill workers on shutdown.
	r.tg.OnStop(func() error {
		id := r.mu.RLock()
		for _, worker := range r.workerPool {
			close(worker.killChan)
		}
		r.mu.RUnlock(id)
		return nil
	})

	return r, nil
}

// New returns an initialized renter.
func New(g modules.Gateway, cs modules.ConsensusSet, wallet modules.Wallet, tpool modules.TransactionPool, persistDir string) (*Renter, error) {
	hdb, err := hostdb.New(g, cs, persistDir)
	if err != nil {
		return nil, err
	}
	hc, err := contractor.New(cs, wallet, tpool, hdb, persistDir)
	if err != nil {
		return nil, err
	}

	return NewCustomRenter(g, cs, tpool, hdb, hc, persistDir, modules.ProdDependencies)
}
