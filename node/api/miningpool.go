package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/HardDriveCoin/HardDriveCoin/modules"
	"github.com/HardDriveCoin/HardDriveCoin/types"
	"github.com/julienschmidt/httprouter"
)

type (
	// PoolGET contains the stats that is returned after a GET request
	// to /pool.
	PoolGET struct {
		PoolRunning  bool `json:"poolrunning"`
		BlocksMined  int  `json:"blocksmined"`
		PoolHashrate int  `json:"poolhashrate"`
	}
	// PoolConfig contains the parameters you can set to config your pool
	PoolConfig struct {
		AcceptingShares    bool             `json:"acceptingshares"`
		OperatorPercentage float64          `json:"operatorpercentage"`
		NetworkPort        uint16           `json:"networkport"`
		DBConnection       string           `json:"dbconnection"`
		Name               string           `json:"name"`
		PoolID             string           `json:"poolid"`
		PoolWallet         types.UnlockHash `json:"poolwallet"`
		OperatorWallet     types.UnlockHash `json:"operatorwallet"`
	}
	PoolClientsInfo struct {
		NumberOfClients uint64           `json:"numberofclients"`
		NumberOfWorkers uint64           `json:"numberofworkers"`
		Clients         []PoolClientInfo `json:"clientinfo"`
	}
	PoolClientInfo struct {
		ClientName  string           `json:"clientname"`
		BlocksMined uint64           `json:"blocksminer"`
		Balance     string           `json:"balance"`
		Workers     []PoolWorkerInfo `json:"workers"`
	}
	PoolClientTransactions struct {
		BalanceChange string    `json:"balancechange"`
		TxTime        time.Time `json:"txtime"`
		Memo          string    `json:"memo"`
	}

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
	PoolBlocksInfo struct {
		BlockNumber uint64    `json:"blocknumber"`
		BlockHeight uint64    `json:"blockheight"`
		BlockReward string    `json:"blockreward"`
		BlockTime   time.Time `json:"blocktime"`
		BlockStatus string    `json:"blockstatus"`
	}
	PoolBlockInfo struct {
		ClientName       string  `json:"clientname"`
		ClientPercentage float64 `json:"clientpercentage"`
		ClientReward     string  `json:"clientreward"`
	}
)

// poolHandler handles the API call that queries the pool's status.
func (api *API) poolHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	pg := PoolGET{
		PoolRunning:  api.pool.GetRunning(),
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
	pg := PoolConfig{
		Name:               settings.PoolName,
		AcceptingShares:    settings.AcceptingShares,
		OperatorPercentage: settings.PoolOperatorPercentage,
		NetworkPort:        settings.PoolNetworkPort,
		DBConnection:       settings.PoolDBConnection,
		PoolID:             settings.PoolID,
		PoolWallet:         settings.PoolWallet,
		OperatorWallet:     settings.PoolOperatorWallet,
	}
	WriteJSON(w, pg)
}

// parsePoolSettings a request's query strings and returns a
// modules.PoolInternalSettings configured with the request's query string
// parameters.
func (api *API) parsePoolSettings(req *http.Request) (modules.PoolInternalSettings, error) {
	settings := api.pool.InternalSettings()

	if req.FormValue("operatorwallet") != "" {
		var x types.UnlockHash
		x, err := scanAddress(req.FormValue("operatorwallet"))
		if err != nil {
			fmt.Println(err)
			return modules.PoolInternalSettings{}, nil
		}
		settings.PoolOperatorWallet = x
	}
	if req.FormValue("poolwallet") != "" {
		var x types.UnlockHash
		x, err := scanAddress(req.FormValue("poolwallet"))
		if err != nil {
			fmt.Println(err)
			return modules.PoolInternalSettings{}, nil
		}
		settings.PoolWallet = x
	}
	if req.FormValue("acceptingshares") != "" {
		var x bool
		_, err := fmt.Sscan(req.FormValue("acceptingshares"), &x)
		if err != nil {
			return modules.PoolInternalSettings{}, nil
		}
		settings.AcceptingShares = x
	}
	if req.FormValue("operatorpercentage") != "" {
		var x float64
		_, err := fmt.Sscan(req.FormValue("operatorpercentage"), &x)
		if err != nil {
			return modules.PoolInternalSettings{}, nil
		}
		settings.PoolOperatorPercentage = x

	}
	if req.FormValue("networkport") != "" {
		var x uint16
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
		settings.PoolID = req.FormValue("poolid")
	}
	if req.FormValue("dbconnection") != "" {
		settings.PoolDBConnection = req.FormValue("dbconnection")
	}
	err := api.pool.SetInternalSettings(settings)
	return settings, err
}

func (api *API) poolGetClientsInfo(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	cd := api.pool.ClientData()
	var nw uint64
	var pc []PoolClientInfo
	for _, c := range cd {
		var pw []PoolWorkerInfo
		for _, wn := range c.Workers {
			worker := PoolWorkerInfo{
				WorkerName:    wn.WorkerName,
				LastShareTime: wn.LastShareTime,
			}
			pw = append(pw, worker)
		}
		client := PoolClientInfo{
			ClientName:  c.ClientName,
			Workers:     pw,
			Balance:     c.Balance,
			BlocksMined: c.BlocksMined,
		}
		pc = append(pc, client)
		nw += uint64(len(pw))
	}
	pci := PoolClientsInfo{
		NumberOfClients: uint64(len(pc)),
		NumberOfWorkers: nw,
		Clients:         pc,
	}
	WriteJSON(w, pci)
}

func (api *API) poolGetClientInfo(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	client := api.pool.FindClient(req.FormValue("name"))
	if client == nil {
		WriteError(w, Error{"error could not find client " + req.FormValue("name")}, http.StatusBadRequest)
		return
	}

	var pw []PoolWorkerInfo
	for _, wn := range client.Workers {
		worker := PoolWorkerInfo{
			WorkerName:             wn.WorkerName,
			LastShareTime:          wn.LastShareTime,
			CurrentDifficulty:      wn.CurrentDifficulty,
			CumulativeDifficulty:   wn.CumulativeDifficulty,
			SharesThisBlock:        wn.SharesThisBlock,
			InvalidSharesThisBlock: wn.InvalidSharesThisBlock,
			StaleSharesThisBlock:   wn.StaleSharesThisBlock,
			BlocksFound:            wn.BlocksFound,
		}
		pw = append(pw, worker)
	}

	pci := PoolClientInfo{
		ClientName:  client.ClientName,
		Balance:     client.Balance,
		BlocksMined: client.BlocksMined,
		Workers:     pw,
	}
	WriteJSON(w, pci)
}

func (api *API) poolGetClientTransactions(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	client := api.pool.ClientTransactions(req.FormValue("name"))
	if client == nil {
		WriteError(w, Error{"error could not find client " + req.FormValue("name")}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, client)
}

func (api *API) poolGetBlocksInfo(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	bi := api.pool.BlocksInfo()
	var pb []PoolBlocksInfo
	for _, b := range bi {
		block := PoolBlocksInfo{
			BlockHeight: b.BlockHeight,
			BlockNumber: b.BlockNumber,
			BlockReward: b.BlockReward,
			BlockTime:   b.BlockTime,
			BlockStatus: b.BlockStatus,
		}
		pb = append(pb, block)
	}
	WriteJSON(w, bi)
}

func (api *API) poolGetBlockInfo(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var block uint64
	fmt.Sscanf(req.FormValue("block"), "%d", &block)
	bi := api.pool.BlockInfo(block)
	if bi == nil {
		WriteError(w, Error{"error could not find block " + req.FormValue("block")}, http.StatusBadRequest)
		return
	}

	WriteJSON(w, bi)
}

// poolStartHandler handles the API call that starts the pool.
func (api *API) poolStartHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	api.pool.StartPool()
	WriteSuccess(w)
}

// poolStopHandler handles the API call to stop the pool.
func (api *API) poolStopHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	api.pool.StopPool()
	WriteSuccess(w)
}
