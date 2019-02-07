package api

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/siafile"

	"github.com/julienschmidt/httprouter"
)

// thirdpartyRenterUploadHandler handles the API call to upload a file.
func (api *API) thirdpartyRenterUploadHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	source, err := url.QueryUnescape(req.FormValue("source"))
	if err != nil {
		WriteError(w, Error{"failed to unescape the source path"}, http.StatusBadRequest)
		return
	}
	if !filepath.IsAbs(source) {
		WriteError(w, Error{"source must be an absolute path"}, http.StatusBadRequest)
		return
	}

	// Check whether existing file should be overwritten
	force := false
	if f := req.FormValue("force"); f != "" {
		force, err = strconv.ParseBool(f)
		if err != nil {
			WriteError(w, Error{"unable to parse 'force' parameter: " + err.Error()}, http.StatusBadRequest)
			return
		}
	}

	// Check whether the erasure coding parameters have been supplied.
	var ec modules.ErasureCoder
	if req.FormValue("datapieces") != "" || req.FormValue("paritypieces") != "" {
		// Check that both values have been supplied.
		if req.FormValue("datapieces") == "" || req.FormValue("paritypieces") == "" {
			WriteError(w, Error{"must provide both the datapieces parameter and the paritypieces parameter if specifying erasure coding parameters"}, http.StatusBadRequest)
			return
		}

		// Parse the erasure coding parameters.
		var dataPieces, parityPieces int
		_, err := fmt.Sscan(req.FormValue("datapieces"), &dataPieces)
		if err != nil {
			WriteError(w, Error{"unable to read parameter 'datapieces': " + err.Error()}, http.StatusBadRequest)
			return
		}
		_, err = fmt.Sscan(req.FormValue("paritypieces"), &parityPieces)
		if err != nil {
			WriteError(w, Error{"unable to read parameter 'paritypieces': " + err.Error()}, http.StatusBadRequest)
			return
		}

		// Verify that sane values for parityPieces and redundancy are being
		// supplied.
		if parityPieces < requiredParityPieces {
			WriteError(w, Error{fmt.Sprintf("a minimum of %v parity pieces is required, but %v parity pieces requested", parityPieces, requiredParityPieces)}, http.StatusBadRequest)
			return
		}
		redundancy := float64(dataPieces+parityPieces) / float64(dataPieces)
		if float64(dataPieces+parityPieces)/float64(dataPieces) < requiredRedundancy {
			WriteError(w, Error{fmt.Sprintf("a redundancy of %.2f is required, but redundancy of %.2f supplied", redundancy, requiredRedundancy)}, http.StatusBadRequest)
			return
		}

		// Create the erasure coder.
		ec, err = siafile.NewRSSubCode(dataPieces, parityPieces, 64)
		if err != nil {
			WriteError(w, Error{"unable to encode file using the provided parameters: " + err.Error()}, http.StatusBadRequest)
			return
		}
	}

	// Call the thirdpartyRenter to upload the file.
	err = api.thirdpartyrenter.Upload(modules.FileUploadParams{
		Source:         source,
		HyperspacePath: strings.TrimPrefix(ps.ByName("hyperspacepath"), "/"),
		ErasureCode:    ec,
		Force:          force,
	})
	if err != nil {
		WriteError(w, Error{"upload failed: " + err.Error()}, http.StatusInternalServerError)
		return
	}
	WriteSuccess(w)
}

// thirdpartyRenterDirHandlerGET handles the API call to create a directory
func (api *API) thirdpartyRenterDirHandlerGET(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	directories, files, err := api.renter.DirList(strings.TrimPrefix(ps.ByName("hyperspacepath"), "/"))
	if err != nil {
		WriteError(w, Error{"failed to get directory contents:" + err.Error()}, http.StatusInternalServerError)
		return
	}
	WriteJSON(w, RenterDirectory{
		Directories: directories,
		Files:       files,
	})
	return
}

// thirdpartyRenterDirHandlerPOST handles the API call to create a directory
func (api *API) thirdpartyRenterDirHandlerPOST(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	// Parse action
	action := req.FormValue("action")
	if action == "" {
		WriteError(w, Error{"you must set the action you wish to execute"}, http.StatusInternalServerError)
		return
	}
	if action == "create" {
		// Call the thirdpartyRenter to create directory
		err := api.renter.CreateDir(strings.TrimPrefix(ps.ByName("hyperspacepath"), "/"))
		if err != nil {
			WriteError(w, Error{"failed to create directory: " + err.Error()}, http.StatusInternalServerError)
			return
		}
		WriteSuccess(w)
		return
	}
	if action == "delete" {
		err := api.renter.DeleteDir(strings.TrimPrefix(ps.ByName("hyperspacepath"), "/"))
		if err != nil {
			WriteError(w, Error{"failed to create directory: " + err.Error()}, http.StatusInternalServerError)
			return
		}
		WriteSuccess(w)
		return
	}
	if action == "rename" {
		fmt.Println("rename")
		// newhyperspacepath := ps.ByName("newhyperspacepath")
		// TODO - implement
		WriteError(w, Error{"not implemented"}, http.StatusNotImplemented)
		return
	}

	// Report that no calls were made
	WriteError(w, Error{"no calls were made, please check your submission and try again"}, http.StatusInternalServerError)
	return
}

// thirdpartyRenterFilesHandler handles the API call to list all of the files.
func (api *API) thirdpartyRenterFilesHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	filter := req.FormValue("filter")
	if len(filter) > 0 {
		r, err := regexp.Compile(filter)
		if err != nil {
			WriteError(w, Error{err.Error()}, http.StatusBadRequest)
			return
		}
		WriteJSON(w, RenterFiles{
			Files: api.thirdpartyrenter.FileList(r),
		})
	} else {
		WriteJSON(w, RenterFiles{
			Files: api.thirdpartyrenter.FileList(),
		})
	}
}
