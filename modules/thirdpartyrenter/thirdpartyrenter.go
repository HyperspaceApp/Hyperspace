// Package thirdpartyrenter is responsible for uploading and downloading files on the sia
// network.
package thirdpartyrenter

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siadir"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siafile"
	"github.com/HyperspaceApp/Hyperspace/modules/thirdpartyrenter/contractor"
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

// A hostContractor negotiates, revises, renews, and provides access to file
// contracts.
type hostContractor interface {

	// Close closes the hostContractor.
	Close() error

	// Contracts returns the staticContracts of the renter's hostContractor.
	Contracts() []modules.RenterContract

	// ContractByPublicKey returns the contract associated with the host key.
	ContractByPublicKey(types.SiaPublicKey) (modules.RenterContract, bool)

	// ContractUtility returns the utility field for a given contract, along
	// with a bool indicating if it exists.
	ContractUtility(types.SiaPublicKey) (modules.ContractUtility, bool)

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
}

// ThirdpartyRenter is the renter for third party contract manegement system
type ThirdpartyRenter struct {
	// File management.
	//
	staticFileSet *siafile.SiaFileSet

	// Directory Management
	//
	staticDirSet *siadir.SiaDirSet

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
	deps              modules.Dependencies
	log               *persist.Logger
	persist           persistence
	persistDir        string
	filesDir          string
	mu                *siasync.RWMutex
	tg                threadgroup.ThreadGroup
	tpool             modules.TransactionPool
	wal               *writeaheadlog.WAL
	hostContractor    hostContractor
}

// Enforce that Renter satisfies the modules.Renter interface.
var _ modules.ThirdpartyRenter = (*ThirdpartyRenter)(nil)

// NewCustomThirdpartyRenter initializes a renter and returns it.
func NewCustomThirdpartyRenter(hc hostContractor, persistDir string, deps modules.Dependencies) (*ThirdpartyRenter, error) {

	r := &ThirdpartyRenter{
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

		deps:           deps,
		persistDir:     persistDir,
		filesDir:       filepath.Join(persistDir, modules.SiapathRoot),
		mu:             siasync.New(modules.SafeMutexDelay, 1),
		hostContractor: hc,
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

	// Spin up the workers for the work pool.
	// r.managedUpdateWorkerPool()
	// go r.threadedDownloadLoop()
	// go r.threadedUploadLoop()

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
func New(persistDir string) (*ThirdpartyRenter, error) {
	hc, err := contractor.New(persistDir)
	if err != nil {
		return nil, err
	}

	return NewCustomThirdpartyRenter(hc, persistDir, modules.ProdDependencies)
}

// Close closes the Renter and its dependencies
func (r *ThirdpartyRenter) Close() error {
	return r.tg.Stop()
}

// setBandwidthLimits will change the bandwidth limits of the renter based on
// the persist values for the bandwidth.
func (r *ThirdpartyRenter) setBandwidthLimits(downloadSpeed int64, uploadSpeed int64) error {
	// Input validation.
	if downloadSpeed < 0 || uploadSpeed < 0 {
		return errors.New("download/upload rate limit can't be below 0")
	}

	// Check for sentinel "no limits" value.
	// if downloadSpeed == 0 && uploadSpeed == 0 {
	// 	r.hostContractor.SetRateLimits(0, 0, 0)
	// } else {
	// 	// Set the rate limits according to the provided values.
	// 	r.hostContractor.SetRateLimits(downloadSpeed, uploadSpeed, 4*4096)
	// }
	return nil
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

// managedContractUtilities grabs the pubkeys of the hosts that the file(s) have
// been uploaded to and then generates maps of the contract's utilities showing
// which hosts are GoodForRenew and which hosts are Offline.  The offline and
// goodforrenew maps are needed for calculating redundancy and other file
// metrics.
func (r *ThirdpartyRenter) managedContractUtilities(entrys []*siafile.SiaFileSetEntry) (offline map[string]bool, goodForRenew map[string]bool) {
	// Save host keys in map.
	pks := make(map[string]types.SiaPublicKey)
	goodForRenew = make(map[string]bool)
	offline = make(map[string]bool)
	for _, e := range entrys {
		var used []types.SiaPublicKey
		for _, pk := range e.HostPublicKeys() {
			pks[string(pk.Key)] = pk
			used = append(used, pk)
		}
		if err := e.UpdateUsedHosts(used); err != nil {
			r.log.Debugln("WARN: Could not update used hosts:", err)
		}
	}

	// Build 2 maps that map every pubkey to its offline and goodForRenew
	// status.
	for _, pk := range pks {
		cu, ok := r.ContractUtility(pk)
		if !ok {
			continue
		}
		goodForRenew[string(pk.Key)] = cu.GoodForRenew
		offline[string(pk.Key)] = r.hostContractor.IsOffline(pk)
	}
	return offline, goodForRenew
}

// ContractUtility returns the utility field for a given contract, along
// with a bool indicating if it exists.
func (r *ThirdpartyRenter) ContractUtility(pk types.SiaPublicKey) (modules.ContractUtility, bool) {
	return r.hostContractor.ContractUtility(pk)
}
