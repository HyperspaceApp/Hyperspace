package thirdpartyapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	nodeapi "github.com/HyperspaceApp/Hyperspace/node/api"

	"github.com/julienschmidt/httprouter"
)

// ExtendedHostDBEntry is an extension to modules.HostDBEntry that includes
// the string representation of the public key, otherwise presented as two
// fields, a string and a base64 encoded byte slice.
// type ExtendedHostDBEntry struct {
// 	modules.HostDBEntry
// 	PublicKeyString string                     `json:"publickeystring"`
// 	ScoreBreakdown  modules.HostScoreBreakdown `json:"scorebreakdown"`
// }

// buildHttpRoutes sets up and returns an * httprouter.Router.
// it connected the Router to the given api using the required
// parameters: requiredUserAgent and requiredPassword
func (api *ThirdpartyAPI) buildHTTPRoutes() error {
	router := httprouter.New()

	router.NotFound = http.HandlerFunc(UnrecognizedCallHandler)
	router.RedirectTrailingSlash = false

	// sign
	// fetch contracts
	router.GET("/contracts", api.renterContractsHandler)
	router.GET("/hostdb/all", api.hostdbAllHandler)

	// update contract info
	// upload/download meta data of fies

	api.router = cleanCloseHandler(router)
	return nil
}

// cleanCloseHandler wraps the entire API, ensuring that underlying conns are
// not leaked if the remote end closes the connection before the underlying
// handler finishes.
func cleanCloseHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close this file handle either when the function completes or when the
		// connection is done.
		done := make(chan struct{})
		go func(w http.ResponseWriter, r *http.Request) {
			defer close(done)
			next.ServeHTTP(w, r)
		}(w, r)
		select {
		case <-done:
		}

		// Sanity check - thread should not take more than an hour to return. This
		// must be done in a goroutine, otherwise the server will not close the
		// underlying socket for this API call.
		timer := time.NewTimer(time.Minute * 60)
		go func() {
			select {
			case <-done:
				timer.Stop()
			case <-timer.C:
				build.Severe("api call is taking more than 60 minutes to return:", r.URL.Path)
			}
		}()
	})
}

// RequireUserAgent is middleware that requires all requests to set a
// UserAgent that contains the specified string.
func RequireUserAgent(h http.Handler, ua string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.Contains(req.UserAgent(), ua) {
			WriteError(w, Error{"Browser access disabled due to security vulnerability. Use Sia-UI or siac."}, http.StatusBadRequest)
			return
		}
		h.ServeHTTP(w, req)
	})
}

func (api *ThirdpartyAPI) renterContractsHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	WriteJSON(w, modules.ThirdpartyRenterContracts{
		Contracts: api.thirdparty.ThirdpartyContracts(),
		Height:    api.cs.Height(),
	})
}

// hostdbAllHandler handles the API call asking for the list of all hosts.
func (api *ThirdpartyAPI) hostdbAllHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get the set of all hosts and convert them into extended hosts.
	hosts := api.thirdparty.AllHosts()
	var extendedHosts []nodeapi.ExtendedHostDBEntry
	for _, host := range hosts {
		extendedHosts = append(extendedHosts, nodeapi.ExtendedHostDBEntry{
			HostDBEntry:     host,
			PublicKeyString: host.PublicKey.String(),
			ScoreBreakdown:  api.thirdparty.ScoreBreakdown(host),
		})
	}

	WriteJSON(w, nodeapi.HostdbAllGET{
		Hosts: extendedHosts,
	})
}
