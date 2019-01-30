package thirdpartyapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"

	"github.com/julienschmidt/httprouter"
)

// buildHttpRoutes sets up and returns an * httprouter.Router.
// it connected the Router to the given api using the required
// parameters: requiredUserAgent and requiredPassword
func (api *ThirdpartyAPI) buildHTTPRoutes() error {
	router := httprouter.New()

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
