package api

import (
	"net/http"

	"github.com/HyperspaceApp/Hyperspace/modules"

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
