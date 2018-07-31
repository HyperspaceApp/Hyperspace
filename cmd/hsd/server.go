package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/consensus"
	"github.com/HyperspaceApp/Hyperspace/modules/explorer"
	"github.com/HyperspaceApp/Hyperspace/modules/gateway"
	"github.com/HyperspaceApp/Hyperspace/modules/host"
	index "github.com/HyperspaceApp/Hyperspace/modules/index"
	"github.com/HyperspaceApp/Hyperspace/modules/miner"
	pool "github.com/HyperspaceApp/Hyperspace/modules/miningpool"
	"github.com/HyperspaceApp/Hyperspace/modules/renter"
	"github.com/HyperspaceApp/Hyperspace/modules/stratumminer"
	"github.com/HyperspaceApp/Hyperspace/modules/transactionpool"
	"github.com/HyperspaceApp/Hyperspace/modules/wallet"
	"github.com/HyperspaceApp/Hyperspace/node/api"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/inconshreveable/go-update"
	"github.com/julienschmidt/httprouter"
	"github.com/kardianos/osext"
)

var errEmptyUpdateResponse = errors.New("API call to https://api.github.com/repos/HyperspaceApp/Hyperspace/releases/latest is returning an empty response")

type (
	// Server creates and serves a HTTP server that offers communication with a
	// Sia API.
	Server struct {
		httpServer    *http.Server
		listener      net.Listener
		config        Config
		moduleClosers []moduleCloser
		api           http.Handler
		mu            sync.Mutex
	}

	// moduleCloser defines a struct that closes modules, defined by a name and
	// an underlying io.Closer.
	moduleCloser struct {
		name string
		io.Closer
	}

	// SiaConstants is a struct listing all of the constants in use.
	SiaConstants struct {
		BlockFrequency         types.BlockHeight `json:"blockfrequency"`
		BlockSizeLimit         uint64            `json:"blocksizelimit"`
		ExtremeFutureThreshold types.Timestamp   `json:"extremefuturethreshold"`
		FutureThreshold        types.Timestamp   `json:"futurethreshold"`
		GenesisTimestamp       types.Timestamp   `json:"genesistimestamp"`
		MaturityDelay          types.BlockHeight `json:"maturitydelay"`
		MedianTimestampWindow  uint64            `json:"mediantimestampwindow"`
		TargetWindow           types.BlockHeight `json:"targetwindow"`

		InitialCoinbase uint64 `json:"initialcoinbase"`
		MinimumCoinbase uint64 `json:"minimumcoinbase"`

		RootTarget types.Target `json:"roottarget"`
		RootDepth  types.Target `json:"rootdepth"`

		// DEPRECATED: same values as MaxTargetAdjustmentUp and
		// MaxTargetAdjustmentDown.
		MaxAdjustmentUp   *big.Rat `json:"maxadjustmentup"`
		MaxAdjustmentDown *big.Rat `json:"maxadjustmentdown"`

		MaxTargetAdjustmentUp   *big.Rat `json:"maxtargetadjustmentup"`
		MaxTargetAdjustmentDown *big.Rat `json:"maxtargetadjustmentdown"`

		SiacoinPrecision types.Currency `json:"siacoinprecision"`
	}

	// DaemonVersion holds the version information for hsd
	DaemonVersion struct {
		Version     string `json:"version"`
		GitRevision string `json:"gitrevision"`
		BuildTime   string `json:"buildtime"`
	}
	// UpdateInfo indicates whether an update is available, and to what
	// version.
	UpdateInfo struct {
		Available bool   `json:"available"`
		Version   string `json:"version"`
	}
	// githubRelease represents some of the JSON returned by the GitHub release API
	// endpoint. Only the fields relevant to updating are included.
	githubRelease struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name        string `json:"name"`
			DownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
)

const (
	// The developer key is used to sign updates and other important Sia-
	// related information.
	developerKey = `-----BEGIN PUBLIC KEY-----
MIIEIjANBgkqhkiG9w0BAQEFAAOCBA8AMIIECgKCBAEAybH8pY5YZIv94jbt97Hf
TR7vEacfSsvah8DFb9rig8p13jZHmO7F6fIrEQALnkelYX8/GWFdwRkUDdZpV63C
+39Fq+6ShGazp/vZ5SJtgq2CEyxftR/gnSFGfiP+2/kuaTgpnMykwRp0S+5fMfkF
8ir5abQQguWtg8E1+dSDd6qVL3SeRygt93onIM/ohnX3MYCZeqIWevUmWOnnRUEZ
pUsiRfgQvMQzFL9fGC7W4JsemAYMZFGrUJm+hEQEuPYJuTy+Gi7PSlmh7+xkDCaL
R8PRv6UtqkT8PRHZwH7aKn/wFx7WMSMxlkuCUIFMwYUXXRd4vui31dFqHkn9e56h
78KeLa35UrPwiDTfjQRkKtc9aW9DZOj8cRbSXh0k5p42N3UnwSocB5r5/CwwpkWX
hH1KYFM0c0mHD/QN6taxUMCdoY+S7Kc486kQPLDj1YJa+1JwB5zBA0lZ3jCUN/Gq
uxXb6Ogds32xl8BWX/HI1AFV/SjBwIztfdddrdsbU0BdTRGqhjyLgNV6vTk+p9KJ
3UiI+762gW1/CNMo5Ck4ERS4f6V8wELS6wsKp+hr8QR+1JSgT8MC39zqzo4Fzpc4
lmMt45fsCngqwoBELZc76mSUhJDKkguhKQBBNE4JBeZXgMzp25PXjIC2D4iS1qT1
fxHoPTjwLcOcx2hK/7kqxjUxxTCz/4Ok3zh3egNAWzqmV1bAOIdxboDiSUmQcMzB
HLoZiPX+Igfbiynrv+d8D/bVA7ob72MdRVJ/1UGkUQ3IKEP7FxoZfDqoiVD2VQ46
7vt4XIatMjbN41wy8JgWTWoNRNmT7EZdOBYUPNi4aN8zkw36zKqX36loyCbN/bio
lug2Zr4X+P8GiAFhAXoFnN8pNuUVQnnVj4Gd5Y6+/2GT3yWmUaMRkawYkpVI2KKn
Ip/yR6ckL4Reg3bx6eR+sVwphNmEhitsMej5HIZ7Wo6QrvHPU3mq56StKsHGNPLC
/f4lFI1ENX4HLgC706XLHgmobSnaue5KmU0OjJG7DxWLhFnLm2iKBOOYkobfXAyO
Z4d6vFXSrOjbFidsZ+/ZRnnOyfn8/Ilf0RBFlHTxTn5hZMfrIivOhugtSFxDyea+
ItZAR2N9R9wkF6KCjMV0cDCWj6dfgoQwIs20A3a79crOo+4l1ytr0E/F1iLpePmF
CQ4Or22eyOrLeDXs51nF+HXZhGkVS/S1pjFcarCDbtdsQYlo6+mjeapYU3ObBelm
X9WNsRRcLpDbXcNmlMAvpqCIToZu9jBFH95bmM6GsTmcGsnF6CYnjE3OTcm5eUIs
IaXSfXDC+sTpH0uUGxuOovbZ9q7D9SILJ6tHxnr2MjR4749oBDre5izswVIJoEse
4wIDAQAB
-----END PUBLIC KEY-----`
)

// version returns the version number of a non-LTS release. This assumes that
// tag names will always be of the form "vX.Y.Z".
func (r *githubRelease) version() string {
	return strings.TrimPrefix(r.TagName, "v")
}

// byVersion sorts non-LTS releases by their version string, placing the highest
// version number first.
type byVersion []githubRelease

func (rs byVersion) Len() int      { return len(rs) }
func (rs byVersion) Swap(i, j int) { rs[i], rs[j] = rs[j], rs[i] }
func (rs byVersion) Less(i, j int) bool {
	// we want the higher version number to reported as "less" so that it is
	// placed first in the slice
	return build.VersionCmp(rs[i].version(), rs[j].version()) >= 0
}

// latestRelease returns the latest non-LTS release, given a set of arbitrary
// releases.
func latestRelease(releases []githubRelease) (githubRelease, error) {
	// filter the releases to exclude LTS releases
	nonLTS := releases[:0]
	for _, r := range releases {
		if !strings.Contains(r.TagName, "lts") && build.IsVersion(r.version()) {
			nonLTS = append(nonLTS, r)
		}
	}

	// sort by version
	sort.Sort(byVersion(nonLTS))

	// return the latest release
	if len(nonLTS) == 0 {
		return githubRelease{}, errEmptyUpdateResponse
	}
	return nonLTS[0], nil
}

// fetchLatestRelease returns metadata about the most recent non-LTS GitHub
// release.
func fetchLatestRelease() (githubRelease, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/HyperspaceApp/Hyperspace/releases", nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	var releases []githubRelease
	err = json.NewDecoder(resp.Body).Decode(&releases)
	if err != nil {
		return githubRelease{}, err
	}
	return latestRelease(releases)
}

// updateToRelease updates hsd and hsc to the release specified. hsc is
// assumed to be in the same folder as hsd.
func updateToRelease(release githubRelease) error {
	updateOpts := update.Options{
		Verifier: update.NewRSAVerifier(),
	}
	err := updateOpts.SetPublicKeyPEM([]byte(developerKey))
	if err != nil {
		// should never happen
		return err
	}

	binaryFolder, err := osext.ExecutableFolder()
	if err != nil {
		return err
	}

	// construct release filename
	releaseName := fmt.Sprintf("Sia-%s-%s-%s.zip", release.TagName, runtime.GOOS, runtime.GOARCH)

	// find release
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == releaseName {
			downloadURL = asset.DownloadURL
			break
		}
	}
	if downloadURL == "" {
		return errors.New("couldn't find download URL for " + releaseName)
	}

	// download release archive
	resp, err := http.Get(downloadURL)
	if err != nil {
		return err
	}
	// release should be small enough to store in memory (<10 MiB); use
	// LimitReader to ensure we don't download more than 32 MiB
	content, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<25))
	resp.Body.Close()
	if err != nil {
		return err
	}
	r := bytes.NewReader(content)
	z, err := zip.NewReader(r, r.Size())
	if err != nil {
		return err
	}

	// process zip, finding hsd/hsc binaries and signatures
	for _, binary := range []string{"hsd", "hsc"} {
		var binData io.ReadCloser
		var signature []byte
		var binaryName string // needed for TargetPath below
		for _, zf := range z.File {
			switch base := path.Base(zf.Name); base {
			case binary, binary + ".exe":
				binaryName = base
				binData, err = zf.Open()
				if err != nil {
					return err
				}
				defer binData.Close()
			case binary + ".sig", binary + ".exe.sig":
				sigFile, err := zf.Open()
				if err != nil {
					return err
				}
				defer sigFile.Close()
				signature, err = ioutil.ReadAll(sigFile)
				if err != nil {
					return err
				}
			}
		}
		if binData == nil {
			return errors.New("could not find " + binary + " binary")
		} else if signature == nil {
			return errors.New("could not find " + binary + " signature")
		}

		// apply update
		updateOpts.Signature = signature
		updateOpts.TargetMode = 0775 // executable
		updateOpts.TargetPath = filepath.Join(binaryFolder, binaryName)
		err = update.Apply(binData, updateOpts)
		if err != nil {
			return err
		}
	}

	return nil
}

// daemonUpdateHandlerGET handles the API call that checks for an update.
func (srv *Server) daemonUpdateHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	release, err := fetchLatestRelease()
	if err != nil {
		api.WriteError(w, api.Error{Message: "Failed to fetch latest release: " + err.Error()}, http.StatusInternalServerError)
		return
	}
	latestVersion := release.TagName[1:] // delete leading 'v'
	api.WriteJSON(w, UpdateInfo{
		Available: build.VersionCmp(latestVersion, build.Version) > 0,
		Version:   latestVersion,
	})
}

// daemonUpdateHandlerPOST handles the API call that updates hsd and hsc.
// There is no safeguard to prevent "updating" to the same release, so callers
// should always check the latest version via daemonUpdateHandlerGET first.
// TODO: add support for specifying version to update to.
func (srv *Server) daemonUpdateHandlerPOST(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	release, err := fetchLatestRelease()
	if err != nil {
		api.WriteError(w, api.Error{Message: "Failed to fetch latest release: " + err.Error()}, http.StatusInternalServerError)
		return
	}
	err = updateToRelease(release)
	if err != nil {
		if rerr := update.RollbackError(err); rerr != nil {
			api.WriteError(w, api.Error{Message: "Serious error: Failed to rollback from bad update: " + rerr.Error()}, http.StatusInternalServerError)
		} else {
			api.WriteError(w, api.Error{Message: "Failed to apply update: " + err.Error()}, http.StatusInternalServerError)
		}
		return
	}
	api.WriteSuccess(w)
}

// debugConstantsHandler prints a json file containing all of the constants.
func (srv *Server) daemonConstantsHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	sc := SiaConstants{
		BlockFrequency:         types.BlockFrequency,
		BlockSizeLimit:         types.BlockSizeLimit,
		ExtremeFutureThreshold: types.ExtremeFutureThreshold,
		FutureThreshold:        types.FutureThreshold,
		GenesisTimestamp:       types.GenesisTimestamp,
		MaturityDelay:          types.MaturityDelay,
		MedianTimestampWindow:  types.MedianTimestampWindow,
		TargetWindow:           types.TargetWindow,

		InitialCoinbase: types.InitialCoinbase,
		MinimumCoinbase: types.MinimumCoinbase,

		RootTarget: types.RootTarget,
		RootDepth:  types.RootDepth,

		// DEPRECATED: same values as MaxTargetAdjustmentUp and
		// MaxTargetAdjustmentDown.
		MaxAdjustmentUp:   types.MaxTargetAdjustmentUp,
		MaxAdjustmentDown: types.MaxTargetAdjustmentDown,

		MaxTargetAdjustmentUp:   types.MaxTargetAdjustmentUp,
		MaxTargetAdjustmentDown: types.MaxTargetAdjustmentDown,

		SiacoinPrecision: types.SiacoinPrecision,
	}

	api.WriteJSON(w, sc)
}

// daemonVersionHandler handles the API call that requests the daemon's version.
func (srv *Server) daemonVersionHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	api.WriteJSON(w, DaemonVersion{Version: build.Version, GitRevision: build.GitRevision, BuildTime: build.BuildTime})
}

// daemonStopHandler handles the API call to stop the daemon cleanly.
func (srv *Server) daemonStopHandler(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	// can't write after we stop the server, so lie a bit.
	api.WriteSuccess(w)

	// need to flush the response before shutting down the server
	f, ok := w.(http.Flusher)
	if !ok {
		panic("Server does not support flushing")
	}
	f.Flush()

	if err := srv.Close(); err != nil {
		build.Critical(err)
	}
}

func (srv *Server) daemonHandler(password string) http.Handler {
	router := httprouter.New()

	router.GET("/daemon/constants", srv.daemonConstantsHandler)
	router.GET("/daemon/version", srv.daemonVersionHandler)
	router.GET("/daemon/update", srv.daemonUpdateHandlerGET)
	router.POST("/daemon/update", srv.daemonUpdateHandlerPOST)
	router.GET("/daemon/stop", api.RequirePassword(srv.daemonStopHandler, password))

	return router
}

// apiHandler handles all calls to the API. If the ready flag is not set, this
// will return an error. Otherwise it will serve the api.
func (srv *Server) apiHandler(w http.ResponseWriter, r *http.Request) {
	srv.mu.Lock()
	isReady := srv.api != nil
	srv.mu.Unlock()
	if !isReady {
		api.WriteError(w, api.Error{Message: "hsd is not ready. please wait for hsd to finish loading."}, http.StatusServiceUnavailable)
		return
	}
	srv.api.ServeHTTP(w, r)
}

// NewServer creates a new net.http server listening on bindAddr.  Only the
// /daemon/ routes are registered by this func, additional routes can be
// registered later by calling serv.mux.Handle.
func NewServer(config Config) (*Server, error) {
	// Process the config variables after they are parsed by cobra.
	config, err := processConfig(config)
	if err != nil {
		return nil, err
	}
	// Create the listener for the server
	l, err := net.Listen("tcp", config.Siad.APIaddr)
	if err != nil {
		if isAddrInUseErr(err) {
			return nil, fmt.Errorf("%v; are you running another instance of hsd?", err.Error())
		}

		return nil, err
	}

	// Create the Server
	mux := http.NewServeMux()
	srv := &Server{
		listener: l,
		httpServer: &http.Server{
			Handler: mux,

			// set reasonable timeout windows for requests, to prevent the Sia API
			// server from leaking file descriptors due to slow, disappearing, or
			// unreliable API clients.

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
		config: config,
	}

	// Register hsd routes
	mux.Handle("/daemon/", api.RequireUserAgent(srv.daemonHandler(config.APIPassword), config.Siad.RequiredUserAgent))
	mux.HandleFunc("/", srv.apiHandler)

	return srv, nil
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

// loadModules loads the modules defined by the server's config and makes their
// API routes available.
func (srv *Server) loadModules() error {
	// Create the server and start serving daemon routes immediately.
	fmt.Printf("(0/%d) Loading hsd...\n", len(srv.config.Siad.Modules))

	// Initialize the Sia modules
	i := 0
	var err error
	var g modules.Gateway
	if strings.Contains(srv.config.Siad.Modules, "g") {
		i++
		fmt.Printf("(%d/%d) Loading gateway...\n", i, len(srv.config.Siad.Modules))
		g, err = gateway.New(srv.config.Siad.RPCaddr, !srv.config.Siad.NoBootstrap, filepath.Join(srv.config.Siad.SiaDir, modules.GatewayDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "gateway", Closer: g})
	}
	var cs modules.ConsensusSet
	if strings.Contains(srv.config.Siad.Modules, "c") {
		i++
		fmt.Printf("(%d/%d) Loading consensus...\n", i, len(srv.config.Siad.Modules))
		cs, err = consensus.New(g, !srv.config.Siad.NoBootstrap, filepath.Join(srv.config.Siad.SiaDir, modules.ConsensusDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "consensus", Closer: cs})
	}
	var tpool modules.TransactionPool
	if strings.Contains(srv.config.Siad.Modules, "t") {
		i++
		fmt.Printf("(%d/%d) Loading transaction pool...\n", i, len(srv.config.Siad.Modules))
		tpool, err = transactionpool.New(cs, g, filepath.Join(srv.config.Siad.SiaDir, modules.TransactionPoolDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "transaction pool", Closer: tpool})
	}
	var e modules.Explorer
	if strings.Contains(srv.config.Siad.Modules, "e") {
		i++
		fmt.Printf("(%d/%d) Loading explorer...\n", i, len(srv.config.Siad.Modules))
		e, err = explorer.New(cs, tpool, filepath.Join(srv.config.Siad.SiaDir, modules.ExplorerDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "explorer", Closer: e})
	}
	var w modules.Wallet
	if strings.Contains(srv.config.Siad.Modules, "w") {
		i++
		fmt.Printf("(%d/%d) Loading wallet...\n", i, len(srv.config.Siad.Modules))
		w, err = wallet.New(cs, tpool, filepath.Join(srv.config.Siad.SiaDir, modules.WalletDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "wallet", Closer: w})
	}
	var m modules.Miner
	if strings.Contains(srv.config.Siad.Modules, "m") {
		i++
		fmt.Printf("(%d/%d) Loading miner...\n", i, len(srv.config.Siad.Modules))
		m, err = miner.New(cs, tpool, w, filepath.Join(srv.config.Siad.SiaDir, modules.MinerDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "miner", Closer: m})
	}
	var h modules.Host
	if strings.Contains(srv.config.Siad.Modules, "h") {
		i++
		fmt.Printf("(%d/%d) Loading host...\n", i, len(srv.config.Siad.Modules))
		h, err = host.New(cs, tpool, w, srv.config.Siad.HostAddr, filepath.Join(srv.config.Siad.SiaDir, modules.HostDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "host", Closer: h})
	}
	var r modules.Renter
	if strings.Contains(srv.config.Siad.Modules, "r") {
		i++
		fmt.Printf("(%d/%d) Loading renter...\n", i, len(srv.config.Siad.Modules))
		r, err = renter.New(g, cs, w, tpool, filepath.Join(srv.config.Siad.SiaDir, modules.RenterDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "renter", Closer: r})
	}
	var p modules.Pool
	if strings.Contains(srv.config.Siad.Modules, "p") {
		i++
		fmt.Printf("(%d/%d) Loading pool...\n", i, len(srv.config.Siad.Modules))
		p, err = pool.New(cs, tpool, g, w, filepath.Join(srv.config.Siad.SiaDir, modules.PoolDir), srv.config.MiningPoolConfig)
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "pool", Closer: p})
	}
	var sm modules.StratumMiner
	if strings.Contains(srv.config.Siad.Modules, "s") {
		i++
		fmt.Printf("(%d/%d) Loading stratum miner...\n", i, len(srv.config.Siad.Modules))
		sm, err = stratumminer.New(filepath.Join(srv.config.Siad.SiaDir, modules.StratumMinerDir))
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "stratumminer", Closer: sm})
	}

	var idx modules.Index
	if strings.Contains(srv.config.Siad.Modules, "i") {
		i++
		fmt.Printf("(%d/%d) Loading index...\n", i, len(srv.config.Siad.Modules))
		idx, err = index.New(cs, tpool, g, w, filepath.Join(srv.config.Siad.SiaDir, modules.IndexDir), srv.config.IndexConfig)
		if err != nil {
			return err
		}
		srv.moduleClosers = append(srv.moduleClosers, moduleCloser{name: "idx", Closer: idx})
	}

	// Create the Sia API
	a := api.New(
		srv.config.Siad.RequiredUserAgent,
		srv.config.APIPassword,
		cs,
		e,
		g,
		h,
		m,
		r,
		tpool,
		w,
		p,
		sm,
		idx,
	)

	// connect the API to the server
	srv.mu.Lock()
	srv.api = a
	srv.mu.Unlock()

	// Attempt to auto-unlock the wallet using the SIA_WALLET_PASSWORD env variable
	if password := os.Getenv("SIA_WALLET_PASSWORD"); password != "" {
		fmt.Println("Sia Wallet Password found, attempting to auto-unlock wallet")
		if err := unlockWallet(w, password); err != nil {
			fmt.Println("Auto-unlock failed.")
		} else {
			fmt.Println("Auto-unlock successful.")
		}
	}

	return nil
}

// Serve starts the HTTP server
func (srv *Server) Serve() error {
	// The server will run until an error is encountered or the listener is
	// closed, via either the Close method or the signal handling above.
	// Closing the listener will result in the benign error handled below.
	err := srv.httpServer.Serve(srv.listener)
	if err != nil && !strings.HasSuffix(err.Error(), "use of closed network connection") {
		return err
	}
	return nil
}

// Close closes the Server's listener, causing the HTTP server to shut down.
func (srv *Server) Close() error {
	var errs []error
	// Close the listener, which will cause Server.Serve() to return.
	if err := srv.listener.Close(); err != nil {
		errs = append(errs, err)
	}
	// Close all of the modules in reverse order
	for i := len(srv.moduleClosers) - 1; i >= 0; i-- {
		m := srv.moduleClosers[i]
		fmt.Printf("Closing %v...\n", m.name)
		if err := m.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return build.JoinErrors(errs, "\n")
}
