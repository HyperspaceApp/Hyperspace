package thirdparty

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/contractor"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/hostdb"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siadir"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siafile"
	api "github.com/HyperspaceApp/Hyperspace/modules/thirdparty/thirdpartyapi"
	"github.com/HyperspaceApp/Hyperspace/persist"
	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/HyperspaceApp/errors"
	"github.com/HyperspaceApp/threadgroup"
	"github.com/HyperspaceApp/writeaheadlog"
	"github.com/julienschmidt/httprouter"
)

var (
	errNilContractor = errors.New("cannot create renter with nil contractor")
	errNilCS         = errors.New("cannot create renter with nil consensus set")
	errNilGateway    = errors.New("cannot create hostdb with nil gateway")
	errNilHdb        = errors.New("cannot create renter with nil hostdb")
	errNilTpool      = errors.New("cannot create renter with nil transaction pool")
)

// ThirdpartyServer is the holder of server info
type ThirdpartyServer struct {
	httpServer *http.Server
	listener   net.Listener
	api        http.Handler
	mu         sync.Mutex
}

// DaemonVersion holds the version information for hsd
type DaemonVersion struct {
	Version     string `json:"version"`
	GitRevision string `json:"gitrevision"`
	BuildTime   string `json:"buildtime"`
}

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

	// ThirdpartyContracts returns the staticContracts of the renter's hostContractor.
	ThirdpartyContracts() []modules.ThirdpartyRenterContract

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

	// OldContracts returns the oldContracts of the renter's hostContractor.
	OldContracts() []modules.RenterContract

	// Editor creates an Editor from the specified contract ID, allowing the
	// insertion, deletion, and modification of sectors.
	Editor(types.SiaPublicKey, <-chan struct{}) (contractor.Editor, error)

	// IsOffline reports whether the specified host is considered offline.
	IsOffline(types.SiaPublicKey) bool

	// Downloader creates a Downloader from the specified contract ID,
	// allowing the retrieval of sectors.
	Downloader(types.SiaPublicKey, <-chan struct{}) (contractor.Downloader, error)

	// RecoverableContracts returns the contracts that the contractor deems
	// recoverable. That means they are not expired yet and also not part of the
	// active contracts. Usually this should return an empty slice unless the host
	// isn't available for recovery or something went wrong.
	RecoverableContracts() []modules.RecoverableContract

	// ResolveIDToPubKey returns the public key of a host given a contract id.
	ResolveIDToPubKey(types.FileContractID) types.SiaPublicKey

	// RateLimits Gets the bandwidth limits for connections created by the
	// contractor and its submodules.
	RateLimits() (readBPS int64, writeBPS int64, packetSize uint64)

	// SetRateLimits sets the bandwidth limits for connections created by the
	// contractor and its submodules.
	SetRateLimits(int64, int64, uint64)
}

// A Thirdparty is responsible for tracking all of the files that a user has
// uploaded to thirdparty platform,
// as well as the locations and health of these files.
//
// TODO: Separate the workerPool to have its own mutex. The workerPool doesn't
// interfere with any of the other fields in the renter, should be fine for it
// to have a separate mutex, that way operations on the worker pool don't block
// operations on other parts of the struct. If we're going to do it that way,
// might make sense to split the worker pool off into it's own struct entirely
// the same way that we split of the memoryManager entirely.
type Thirdparty struct {
	// File management.
	//
	staticFileSet *siafile.SiaFileSet

	// Directory Management
	//
	staticDirSet *siadir.SiaDirSet

	// List of workers that can be used for uploading and/or downloading.
	// memoryManager *memoryManager
	// workerPool map[types.FileContractID]*worker

	// Cache the hosts from the last price estimation result.
	lastEstimationHosts []modules.HostDBEntry

	cs             modules.ConsensusSet
	deps           modules.Dependencies
	g              modules.Gateway
	hostContractor hostContractor
	hostDB         hostDB
	log            *persist.Logger
	persistDir     string
	filesDir       string
	mu             *siasync.RWMutex
	tg             threadgroup.ThreadGroup
	tpool          modules.TransactionPool
	wal            *writeaheadlog.WAL

	server *ThirdpartyServer
}

// Close closes the Renter and its dependencies
func (t *Thirdparty) Close() error {
	t.tg.Stop()
	t.hostDB.Close()
	return t.hostContractor.Close()
}

// Enforce that Renter satisfies the modules.Renter interface.
// var _ modules.Renter = (*Renter)(nil)

// NewCustomThirdparty initializes a renter and returns it.
func NewCustomThirdparty(g modules.Gateway, cs modules.ConsensusSet, tpool modules.TransactionPool, hdb hostDB, hc hostContractor,
	persistDir string, deps modules.Dependencies) (*Thirdparty, error) {
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

	t := &Thirdparty{
		cs:             cs,
		deps:           deps,
		g:              g,
		hostDB:         hdb,
		hostContractor: hc,
		persistDir:     persistDir,
		filesDir:       filepath.Join(persistDir, modules.SiapathRoot),
		mu:             siasync.New(modules.SafeMutexDelay, 1),
		tpool:          tpool,
	}

	server, err := newThirdpartyServer()
	if err != nil {
		return nil, err
	}
	go func() {
		server.Serve()
	}()

	t.server = server
	a, err := api.New(t)
	if err != nil {
		return nil, err
	}
	server.mu.Lock()
	server.api = a
	server.mu.Unlock()

	// t.memoryManager = newMemoryManager(defaultMemory, t.tg.StopChan())

	// Load all saved data.
	// if err := t.initPersist(); err != nil {
	// 	return nil, err
	// }

	// Initialize the streaming cache.
	// t.staticStreamCache = newStreamCache(r.persist.StreamCacheSize)

	if cs.SpvMode() {
		// Subscribe to the consensus set.
		// err = cs.HeaderConsensusSetSubscribe(r, modules.ConsensusChangeRecent, t.tg.StopChan())
		// if err != nil {
		// 	return nil, err
		// }
		panic("need full node to run third party contract mangement system")
	} else {
		// Subscribe to the consensus set.
		err := cs.ConsensusSetSubscribe(t, modules.ConsensusChangeRecent, t.tg.StopChan())
		if err != nil {
			return nil, err
		}
	}

	// Spin up the workers for the work pool.
	// t.managedUpdateWorkerPool()
	// go t.threadedDownloadLoop()
	// go t.threadedUploadLoop()

	// Kill workers on shutdown.
	t.tg.OnStop(func() error {
		id := t.mu.RLock()
		// for _, worker := range t.workerPool {
		// 	close(worker.killChan)
		// }
		t.mu.RUnlock(id)
		return nil
	})

	go func() {
		for {
			if t.cs.Synced() {
				err := t.hostContractor.SetAllowance(modules.DefaultAllowance)
				if err != nil {
					panic(err)
				}
				break
			}
			time.Sleep(time.Second)
		}
	}()

	return t, nil
}

// New returns an initialized thirdparty.
func New(g modules.Gateway, cs modules.ConsensusSet, wallet modules.Wallet, tpool modules.TransactionPool, persistDir string) (*Thirdparty, error) {
	hdb, err := hostdb.New(g, cs, tpool, persistDir)
	if err != nil {
		return nil, err
	}
	hc, err := contractor.New(cs, wallet, tpool, hdb, persistDir)
	if err != nil {
		return nil, err
	}
	return NewCustomThirdparty(g, cs, tpool, hdb, hc, persistDir, modules.ProdDependencies)
}

// Contracts returns an array of host contractor's staticContracts
func (t *Thirdparty) Contracts() []modules.RenterContract {
	return t.hostContractor.Contracts()
}

// ThirdpartyContracts returns an array of host contractor's staticContracts
func (t *Thirdparty) ThirdpartyContracts() []modules.ThirdpartyRenterContract {
	return t.hostContractor.ThirdpartyContracts()
}

// SetSettings will update the settings for the renter.
//
// NOTE: This function can't be atomic. Typically we try to have user requests
// be atomic, so that either everything changes or nothing changes, but since
// these changes happen progressively, it's possible for some of the settings
// (like the allowance) to succeed, but then if the bandwidth limits for example
// are bad, then the allowance will update but the bandwidth will not update.
func (t *Thirdparty) SetSettings(s modules.RenterSettings) error {
	// Early input validation.
	if s.MaxDownloadSpeed < 0 || s.MaxUploadSpeed < 0 {
		return errors.New("bandwidth limits cannot be negative")
	}
	if s.StreamCacheSize <= 0 {
		return errors.New("stream cache size needs to be 1 or larger")
	}

	// Set allowance.
	err := t.hostContractor.SetAllowance(s.Allowance)
	if err != nil {
		return err
	}

	// Set IPViolationsCheck
	t.hostDB.SetIPViolationCheck(s.IPViolationsCheck)

	// Save the changes.
	err = t.saveSync()
	if err != nil {
		return err
	}

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

// ActiveHosts returns an array of hostDB's active hosts
func (t *Thirdparty) ActiveHosts() []modules.HostDBEntry { return t.hostDB.ActiveHosts() }

// AllHosts returns an array of all hosts
func (t *Thirdparty) AllHosts() []modules.HostDBEntry { return t.hostDB.AllHosts() }

// SetFilterMode sets the renter's hostdb filter mode
func (t *Thirdparty) SetFilterMode(lm modules.FilterMode, hosts []types.SiaPublicKey) error {
	// Check to see how many hosts are needed for the allowance
	minHosts := t.Settings().Allowance.Hosts
	if len(hosts) < int(minHosts) && lm == modules.HostDBActiveWhitelist {
		t.log.Printf("WARN: There are fewer whitelisted hosts than the allowance requires.  Have %v whitelisted hosts, need %v to support allowance\n", len(hosts), minHosts)
	}

	// Set list mode filter for the hostdb
	if err := t.hostDB.SetFilterMode(lm, hosts); err != nil {
		return err
	}

	return nil
}

// Host returns the host associated with the given public key
func (t *Thirdparty) Host(spk types.SiaPublicKey) (modules.HostDBEntry, bool) {
	return t.hostDB.Host(spk)
}

// InitialScanComplete returns a boolean indicating if the initial scan of the
// hostdb is completed.
func (t *Thirdparty) InitialScanComplete() (bool, error) { return t.hostDB.InitialScanComplete() }

// ScoreBreakdown returns the score breakdown
func (t *Thirdparty) ScoreBreakdown(e modules.HostDBEntry) modules.HostScoreBreakdown {
	return t.hostDB.ScoreBreakdown(e)
}

// EstimateHostScore returns the estimated host score
func (t *Thirdparty) EstimateHostScore(e modules.HostDBEntry, a modules.Allowance) modules.HostScoreBreakdown {
	// if reflect.DeepEqual(a, modules.Allowance{}) {
	// 	a = t.Settings().Allowance
	// }
	if reflect.DeepEqual(a, modules.Allowance{}) {
		a = modules.DefaultAllowance
	}
	return t.hostDB.EstimateHostScore(e, a)
}

// Settings returns the renter's allowance
func (t *Thirdparty) Settings() modules.RenterSettings {
	return modules.RenterSettings{
		Allowance:         t.hostContractor.Allowance(),
		IPViolationsCheck: t.hostDB.IPViolationsCheck(),
	}
}

// OldContracts returns an array of host contractor's oldContracts
func (t *Thirdparty) OldContracts() []modules.RenterContract {
	return t.hostContractor.OldContracts()
}

// PeriodSpending returns the host contractor's period spending
func (t *Thirdparty) PeriodSpending() modules.ContractorSpending {
	return t.hostContractor.PeriodSpending()
}

// RecoverableContracts returns the host contractor's recoverable contracts.
func (t *Thirdparty) RecoverableContracts() []modules.RecoverableContract {
	return t.hostContractor.RecoverableContracts()
}

// ProcessConsensusChange returns the process consensus change
func (t *Thirdparty) ProcessConsensusChange(cc modules.ConsensusChange) {

}

// isAddrInUseErr checks if the error corresponds to syscall.EADDRINUSE
func isAddrInUseErr(err error) bool {
	if opErr, ok := err.(*net.OpError); ok {
		if syscallErr, ok := opErr.Err.(*os.SyscallError); ok {
			return syscallErr.Err == syscall.EADDRINUSE
		}
	}
	return false
}

// newThirdpartyServer create a new http server
func newThirdpartyServer() (*ThirdpartyServer, error) {
	l, err := net.Listen("tcp", "0.0.0.0:5585")
	if err != nil {
		if isAddrInUseErr(err) {
			return nil, fmt.Errorf("%v; are you running another instance of hsd?", err.Error())
		}
		return nil, err
	}

	// Create the Server
	mux := http.NewServeMux()
	srv := &ThirdpartyServer{
		listener: l,
		httpServer: &http.Server{
			Handler: mux,

			// ReadTimeout defines the maximum amount of time allowed to fully read
			// the request body. This timeout is applied to every handler in the
			// server.
			ReadTimeout: time.Minute * 5,

			// ReadHeaderTimeout defines the amount of time allowed to fully read the
			// request headers.
			ReadHeaderTimeout: time.Minute * 2,

			// IdleTimeout defines the maximum duration a HTTP Keep-Alive connection
			// the API is kept open with no activity before closing.
			IdleTimeout: time.Minute * 5,
		},
	}

	mux.Handle("/daemon/", api.RequireUserAgent(srv.daemonHandler(), api.RequiredUserAgent))
	mux.HandleFunc("/", srv.apiHandler)

	return srv, nil
}

// Serve starts the HTTP server
func (srv *ThirdpartyServer) Serve() error {
	// The server will run until an error is encountered or the listener is
	// closed, via either the Close method or the signal handling above.
	// Closing the listener will result in the benign error handled below.
	err := srv.httpServer.Serve(srv.listener)
	if err != nil && !strings.HasSuffix(err.Error(), "use of closed network connection") {
		return err
	}
	return nil
}

func (srv *ThirdpartyServer) daemonHandler() http.Handler {
	router := httprouter.New()

	router.GET("/daemon/version", srv.daemonVersionHandler)

	return router
}

// daemonVersionHandler handles the API call that requests the daemon's version.
func (srv *ThirdpartyServer) daemonVersionHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	version := build.Version
	if build.ReleaseTag != "" {
		version += "-" + build.ReleaseTag
	}
	api.WriteJSON(w, DaemonVersion{Version: version, GitRevision: build.GitRevision, BuildTime: build.BuildTime})
}

// apiHandler handles all calls to the API. If the ready flag is not set, this
// will return an error. Otherwise it will serve the api.
func (srv *ThirdpartyServer) apiHandler(w http.ResponseWriter, r *http.Request) {
	srv.mu.Lock()
	isReady := srv.api != nil
	srv.mu.Unlock()
	if !isReady {
		api.WriteError(w, api.Error{Message: "hsd is not ready. please wait for hsd to finish loading."}, http.StatusServiceUnavailable)
		return
	}
	srv.api.ServeHTTP(w, r)
}

// Close closes the Server's listener, causing the HTTP server to shut down.
func (srv *ThirdpartyServer) Close() error {
	return srv.listener.Close()
}
