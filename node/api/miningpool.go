package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/julienschmidt/httprouter"
)

type (
	// MiningPoolGET contains the stats that are returned after a GET request
	// to /pool.
	MiningPoolGET struct {
		BlocksMined  int `json:"blocksmined"`
		PoolHashrate int `json:"poolhashrate"`
	}
	// MiningPoolConfig contains the parameters you can set to config your pool
	MiningPoolConfig struct {
		NetworkPort    int              `json:"networkport"`
		DBConnection   string           `json:"dbconnection"`
		Name           string           `json:"name"`
		PoolID         uint64           `json:"poolid"`
		PoolWallet     types.UnlockHash `json:"poolwallet"`
		OperatorWallet types.UnlockHash `json:"operatorwallet"`
	}
	// MiningPoolClientsInfo returns the stats are return after a GET request
	// to /pool/clients
	MiningPoolClientsInfo struct {
		NumberOfClients uint64                 `json:"numberofclients"`
		NumberOfWorkers uint64                 `json:"numberofworkers"`
		Clients         []MiningPoolClientInfo `json:"clientinfo"`
	}
	// MiningPoolClientInfo returns the stats for a single client
	MiningPoolClientInfo struct {
		ClientName  string           `json:"clientname"`
		BlocksMined uint64           `json:"blocksminer"`
		Balance     string           `json:"balance"`
		Workers     []PoolWorkerInfo `json:"workers"`
	}
	// MiningPoolClientTransaction returns info for a single transaction
	MiningPoolClientTransaction struct {
		BalanceChange string    `json:"balancechange"`
		TxTime        time.Time `json:"txtime"`
		Memo          string    `json:"memo"`
	}
	// PoolWorkerInfo returns info about one of a client's workers
	PoolWorkerInfo struct {
		WorkerName             string    `json:"workername"`
		LastShareTime          time.Time `json:"lastsharetime"`
		CurrentDifficulty      float64   `json:"currentdifficult"`
		CumulativeDifficulty   float64   `json:"cumulativedifficulty"`
		SharesThisBlock        uint64    `json:"sharesthisblock"`
		InvalidSharesThisBlock uint64    `json:"invalidsharesthisblock"`
		StaleSharesThisBlock   uint64    `json:"stalesharesthisblock"`
		BlocksFound            uint64    `json:"blocksfound"`
	}
	// MiningPoolBlockInfo returns info about one of the pool's blocks
	MiningPoolBlockInfo struct {
		BlockNumber uint64    `json:"blocknumber"`
		BlockHeight uint64    `json:"blockheight"`
		BlockReward string    `json:"blockreward"`
		BlockTime   time.Time `json:"blocktime"`
		BlockStatus string    `json:"blockstatus"`
	}
	// MiningPoolBlockClientInfo returns info about one of the pool's block's clients
	MiningPoolBlockClientInfo struct {
		ClientName       string  `json:"clientname"`
		ClientPercentage float64 `json:"clientpercentage"`
		ClientReward     string  `json:"clientreward"`
	}
)

// poolHandler handles the API call that queries the pool's status.
func (api *API) poolHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	pg := MiningPoolGET{
		BlocksMined:  0,
		PoolHashrate: 0,
	}
	WriteJSON(w, pg)
}

// poolConfigHandlerPOST handles POST request to the /pool API endpoint, which sets
// the internal settings of the pool.
func (api *API) poolConfigHandlerPOST(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	settings, err := api.parsePoolSettings(req)
	if err != nil {
		WriteError(w, Error{"error parsing pool settings: " + err.Error()}, http.StatusBadRequest)
		return
	}
	err = api.pool.SetInternalSettings(settings)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// poolConfigHandler handles the API call that queries the pool's status.
func (api *API) poolConfigHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	settings, err := api.parsePoolSettings(req)
	if err != nil {
		WriteError(w, Error{"error parsing pool settings: " + err.Error()}, http.StatusBadRequest)
		return
	}
	pg := MiningPoolConfig{
		Name:         settings.PoolName,
		NetworkPort:  settings.PoolNetworkPort,
		DBConnection: settings.PoolDBConnection,
		PoolID:       settings.PoolID,
		PoolWallet:   settings.PoolWallet,
	}
	WriteJSON(w, pg)
}

// parsePoolSettings a request's query strings and returns a
// modules.PoolInternalSettings configured with the request's query string
// parameters.
func (api *API) parsePoolSettings(req *http.Request) (modules.PoolInternalSettings, error) {
	settings := api.pool.InternalSettings()

	if req.FormValue("poolwallet") != "" {
		var x types.UnlockHash
		x, err := scanAddress(req.FormValue("poolwallet"))
		if err != nil {
			fmt.Println(err)
			return modules.PoolInternalSettings{}, nil
		}
		settings.PoolWallet = x
	}
	if req.FormValue("networkport") != "" {
		var x int
		_, err := fmt.Sscan(req.FormValue("networkport"), &x)
		if err != nil {
			return modules.PoolInternalSettings{}, nil
		}
		settings.PoolNetworkPort = x

	}
	if req.FormValue("name") != "" {
		settings.PoolName = req.FormValue("name")
	}
	if req.FormValue("poolid") != "" {
		var x int
		_, err := fmt.Sscan(req.FormValue("poolid"), &x)
		if err != nil {
			return modules.PoolInternalSettings{}, nil
		}
		settings.PoolID = uint64(x)
	}
	if req.FormValue("dbconnection") != "" {
		settings.PoolDBConnection = req.FormValue("dbconnection")
	}
	err := api.pool.SetInternalSettings(settings)
	return settings, err
}
