package api

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
)

type (
	// StratumMinerGET contains the information that is returned after a GET request
	// to /stratumminer.
	StratumMinerGET struct {
		Hashrate    float64 `json:"hashrate"`
		Mining      bool    `json:"mining"`
		Submissions uint64  `json:"submissions"`
	}
)

// stratumminerHandler handles the API call that queries the stratum miner's status.
func (api *API) stratumminerHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	smg := StratumMinerGET{
		Hashrate:    api.stratumminer.Hashrate(),
		Mining:      api.stratumminer.Mining(),
		Submissions: api.stratumminer.Submissions(),
	}
	WriteJSON(w, smg)
}

// stratumminerStartHandler handles the API call that starts the stratum miner.
func (api *API) stratumminerStartHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var server, username string
	if server = req.FormValue("server"); server == "" {
		WriteError(w, Error{"need to specify a server"}, http.StatusBadRequest)
		return
	}
	if username = req.FormValue("username"); username == "" {
		WriteError(w, Error{"need to specify a username"}, http.StatusBadRequest)
		return
	}
	api.stratumminer.StartStratumMining(server, username)
	WriteSuccess(w)
}

// stratumminerStopHandler handles the API call to stop the stratum miner.
func (api *API) stratumminerStopHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	api.stratumminer.StopStratumMining()
	WriteSuccess(w)
}
