package contractor

import (
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// contractorPersist defines what Contractor data persists across sessions.
type contractorPersist struct {
	Allowance            modules.Allowance               `json:"allowance"`
	BlockHeight          types.BlockHeight               `json:"blockheight"`
	CurrentPeriod        types.BlockHeight               `json:"currentperiod"`
	LastChange           modules.ConsensusChangeID       `json:"lastchange"`
	OldContracts         []modules.RenterContract        `json:"oldcontracts"`
	RecoverableContracts []modules.RecoverableContract   `json:"recoverablecontracts"`
	RenewedFrom          map[string]types.FileContractID `json:"renewedfrom"`
	RenewedTo            map[string]types.FileContractID `json:"renewedto"`
}

// persistData returns the data in the Contractor that will be saved to disk.
func (c *Contractor) persistData() contractorPersist {
	// TODO: save some other data
	// data := contractorPersist{
	// 	Allowance:            c.allowance,
	// 	BlockHeight:          c.blockHeight,
	// 	CurrentPeriod:        c.currentPeriod,
	// 	LastChange:           c.lastChange,
	// 	RecoverableContracts: c.recoverableContracts,
	// 	RenewedFrom:          make(map[string]types.FileContractID),
	// 	RenewedTo:            make(map[string]types.FileContractID),
	// }
	// for k, v := range c.renewedFrom {
	// 	data.RenewedFrom[k.String()] = v
	// }
	// for k, v := range c.renewedTo {
	// 	data.RenewedTo[k.String()] = v
	// }
	// for _, contract := range c.oldContracts {
	// 	data.OldContracts = append(data.OldContracts, contract)
	// }
	return contractorPersist{}
}

// load loads the Contractor persistence data from disk.
func (c *Contractor) load() error {
	// var data contractorPersist
	// err := c.persist.load(&data)
	// if err != nil {
	// 	return err
	// }

	// // COMPATv136 if the allowance is not the empty allowance and "Expected"
	// // fields are not set, set them to the default values.
	// if !reflect.DeepEqual(data.Allowance, modules.Allowance{}) {
	// 	if data.Allowance.ExpectedStorage == 0 && data.Allowance.ExpectedUpload == 0 &&
	// 		data.Allowance.ExpectedDownload == 0 && data.Allowance.ExpectedRedundancy == 0 {
	// 		// Set the fields to the defauls.
	// 		data.Allowance.ExpectedStorage = modules.DefaultAllowance.ExpectedStorage
	// 		data.Allowance.ExpectedUpload = modules.DefaultAllowance.ExpectedUpload
	// 		data.Allowance.ExpectedDownload = modules.DefaultAllowance.ExpectedDownload
	// 		data.Allowance.ExpectedRedundancy = modules.DefaultAllowance.ExpectedRedundancy
	// 	}
	// }

	// c.allowance = data.Allowance
	// c.blockHeight = data.BlockHeight
	// c.currentPeriod = data.CurrentPeriod
	// c.lastChange = data.LastChange
	// c.recoverableContracts = data.RecoverableContracts
	// var fcid types.FileContractID
	// for k, v := range data.RenewedFrom {
	// 	if err := fcid.LoadString(k); err != nil {
	// 		return err
	// 	}
	// 	c.renewedFrom[fcid] = v
	// }
	// for k, v := range data.RenewedTo {
	// 	if err := fcid.LoadString(k); err != nil {
	// 		return err
	// 	}
	// 	c.renewedTo[fcid] = v
	// }
	// for _, contract := range data.OldContracts {
	// 	c.oldContracts[contract.ID] = contract
	// }

	return nil
}

// save saves the Contractor persistence data to disk.
func (c *Contractor) save() error {
	// return c.persist.save(c.persistData())
	return nil
}

// saveSync saves the Contractor persistence data to disk and then syncs to disk.
func (c *Contractor) saveSync() error {
	// return c.persist.save(c.persistData())
	return nil
}
