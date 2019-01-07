package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/julienschmidt/httprouter"
)

func (api *API) thirdpartyContractsHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Parse flags
	var inactive, expired, recoverable bool
	var err error
	if s := req.FormValue("inactive"); s != "" {
		inactive, err = scanBool(s)
		if err != nil {
			WriteError(w, Error{"unable to parse inactive:" + err.Error()}, http.StatusBadRequest)
			return
		}
	}
	if s := req.FormValue("expired"); s != "" {
		expired, err = scanBool(s)
		if err != nil {
			WriteError(w, Error{"unable to parse expired:" + err.Error()}, http.StatusBadRequest)
			return
		}
	}
	if s := req.FormValue("recoverable"); s != "" {
		recoverable, err = scanBool(s)
		if err != nil {
			WriteError(w, Error{"unable to parse recoverable:" + err.Error()}, http.StatusBadRequest)
			return
		}
	}

	// Get current block height for reference
	blockHeight := api.cs.Height()

	// Get active contracts
	contracts := []RenterContract{}
	activeContracts := []RenterContract{}
	inactiveContracts := []RenterContract{}
	expiredContracts := []RenterContract{}
	for _, c := range api.thirdparty.Contracts() {
		var size uint64
		if len(c.Transaction.FileContractRevisions) != 0 {
			size = c.Transaction.FileContractRevisions[0].NewFileSize
		}

		// Fetch host address
		var netAddress modules.NetAddress
		hdbe, exists := api.thirdparty.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.NetAddress
		}

		// Build the contract.
		contract := RenterContract{
			DownloadSpending:          c.DownloadSpending,
			EndHeight:                 c.EndHeight,
			Fees:                      c.TxnFee.Add(c.ContractFee),
			GoodForUpload:             c.Utility.GoodForUpload,
			GoodForRenew:              c.Utility.GoodForRenew,
			HostPublicKey:             c.HostPublicKey,
			ID:                        c.ID,
			LastTransaction:           c.Transaction,
			NetAddress:                netAddress,
			RenterFunds:               c.RenterFunds,
			Size:                      size,
			StartHeight:               c.StartHeight,
			StorageSpending:           c.StorageSpending,
			StorageSpendingDeprecated: c.StorageSpending,
			TotalCost:                 c.TotalCost,
			UploadSpending:            c.UploadSpending,
		}
		if c.Utility.GoodForRenew {
			activeContracts = append(activeContracts, contract)
		} else if inactive && !c.Utility.GoodForRenew {
			inactiveContracts = append(inactiveContracts, contract)
		}
		contracts = append(contracts, contract)
	}

	// Get expired contracts
	if expired || inactive {
		for _, c := range api.renter.OldContracts() {
			var size uint64
			if len(c.Transaction.FileContractRevisions) != 0 {
				size = c.Transaction.FileContractRevisions[0].NewFileSize
			}

			// Fetch host address
			var netAddress modules.NetAddress
			hdbe, exists := api.renter.Host(c.HostPublicKey)
			if exists {
				netAddress = hdbe.NetAddress
			}

			contract := RenterContract{
				DownloadSpending:          c.DownloadSpending,
				EndHeight:                 c.EndHeight,
				Fees:                      c.TxnFee.Add(c.ContractFee),
				GoodForUpload:             c.Utility.GoodForUpload,
				GoodForRenew:              c.Utility.GoodForRenew,
				HostPublicKey:             c.HostPublicKey,
				ID:                        c.ID,
				LastTransaction:           c.Transaction,
				NetAddress:                netAddress,
				RenterFunds:               c.RenterFunds,
				Size:                      size,
				StartHeight:               c.StartHeight,
				StorageSpending:           c.StorageSpending,
				StorageSpendingDeprecated: c.StorageSpending,
				TotalCost:                 c.TotalCost,
				UploadSpending:            c.UploadSpending,
			}
			if expired && c.EndHeight < blockHeight {
				expiredContracts = append(expiredContracts, contract)
			} else if inactive && c.EndHeight >= blockHeight {
				inactiveContracts = append(inactiveContracts, contract)
			}
		}
	}

	var recoverableContracts []modules.RecoverableContract
	if recoverable {
		recoverableContracts = api.renter.RecoverableContracts()
	}

	WriteJSON(w, RenterContracts{
		Contracts:            contracts,
		ActiveContracts:      activeContracts,
		InactiveContracts:    inactiveContracts,
		ExpiredContracts:     expiredContracts,
		RecoverableContracts: recoverableContracts,
	})
}

// hostdbHandler handles the API call asking for the list of active
// hosts.
func (api *API) thirdpartyHostdbHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	isc, err := api.thirdparty.InitialScanComplete()
	if err != nil {
		WriteError(w, Error{"Failed to get initial scan status" + err.Error()}, http.StatusInternalServerError)
		return
	}
	WriteJSON(w, HostdbGet{
		InitialScanComplete: isc,
	})
}

// hostdbActiveHandler handles the API call asking for the list of active
// hosts.
func (api *API) thirdpartyHostdbActiveHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var numHosts uint64
	hosts := api.thirdparty.ActiveHosts()

	if req.FormValue("numhosts") == "" {
		// Default value for 'numhosts' is all of them.
		numHosts = uint64(len(hosts))
	} else {
		// Parse the value for 'numhosts'.
		_, err := fmt.Sscan(req.FormValue("numhosts"), &numHosts)
		if err != nil {
			WriteError(w, Error{err.Error()}, http.StatusBadRequest)
			return
		}

		// Catch any boundary errors.
		if numHosts > uint64(len(hosts)) {
			numHosts = uint64(len(hosts))
		}
	}

	// Convert the entries into extended entries.
	var extendedHosts []ExtendedHostDBEntry
	for _, host := range hosts {
		extendedHosts = append(extendedHosts, ExtendedHostDBEntry{
			HostDBEntry:     host,
			PublicKeyString: host.PublicKey.String(),
			ScoreBreakdown:  api.renter.ScoreBreakdown(host),
		})
	}

	WriteJSON(w, HostdbActiveGET{
		Hosts: extendedHosts[:numHosts],
	})
}

// hostdbAllHandler handles the API call asking for the list of all hosts.
func (api *API) thirdpartyHostdbAllHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get the set of all hosts and convert them into extended hosts.
	hosts := api.thirdparty.AllHosts()
	var extendedHosts []ExtendedHostDBEntry
	for _, host := range hosts {
		extendedHosts = append(extendedHosts, ExtendedHostDBEntry{
			HostDBEntry:     host,
			PublicKeyString: host.PublicKey.String(),
			ScoreBreakdown:  api.renter.ScoreBreakdown(host),
		})
	}

	WriteJSON(w, HostdbAllGET{
		Hosts: extendedHosts,
	})
}

// hostdbHostsHandler handles the API call asking for a specific host,
// returning detailed information about that host.
func (api *API) thirdpartyHostdbHostsHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	var pk types.SiaPublicKey
	pk.LoadString(ps.ByName("pubkey"))

	entry, exists := api.thirdparty.Host(pk)
	if !exists {
		WriteError(w, Error{"requested host does not exist"}, http.StatusBadRequest)
		return
	}
	breakdown := api.thirdparty.ScoreBreakdown(entry)

	// Extend the hostdb entry  to have the public key string.
	extendedEntry := ExtendedHostDBEntry{
		HostDBEntry:     entry,
		PublicKeyString: entry.PublicKey.String(),
	}
	WriteJSON(w, HostdbHostsGET{
		Entry:          extendedEntry,
		ScoreBreakdown: breakdown,
	})
}

// hostdbFilterModeHandlerPOST handles the API call to set the hostdb's filter
// mode
func (api *API) thirdpartyHostdbFilterModeHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Parse parameters
	var params HostdbFilterModePOST
	err := json.NewDecoder(req.Body).Decode(&params)
	if err != nil {
		WriteError(w, Error{"invalid parameters: " + err.Error()}, http.StatusBadRequest)
		return
	}

	var fm modules.FilterMode
	if err = fm.FromString(params.FilterMode); err != nil {
		WriteError(w, Error{"unable to load filter mode from string: " + err.Error()}, http.StatusBadRequest)
		return
	}

	// Set list mode
	if err := api.thirdparty.SetFilterMode(fm, params.Hosts); err != nil {
		WriteError(w, Error{"failed to set the list mode: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}
