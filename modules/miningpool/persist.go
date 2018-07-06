package pool

import (
	"os"
	"path/filepath"

	"github.com/sasha-s/go-deadlock"

	"github.com/HyperspaceApp/Hyperspace/config"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// persistence is the data that is kept when the pool is restarted.
type persistence struct {
	mu deadlock.RWMutex

	// Consensus Tracking.
	BlockHeight  types.BlockHeight         `json:"blockheight"`
	RecentChange modules.ConsensusChangeID `json:"recentchange"`

	// Pool Identity.
	PublicKey      types.SiaPublicKey           `json:"publickey"`
	RevisionNumber uint64                       `json:"revisionnumber"`
	Settings       modules.PoolInternalSettings `json:"settings"`
	UnlockHash     types.UnlockHash             `json:"unlockhash"`

	// Block info
	Target types.Target `json:"blocktarget"`
	// Address       types.UnlockHash `json:"pooladdress"`
	BlocksFound []types.BlockID `json:"blocksfound"`
}

func (p *persistence) GetBlockHeight() types.BlockHeight {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.BlockHeight
}

func (p *persistence) SetBlockHeight(bh types.BlockHeight) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BlockHeight = bh
}

func (p *persistence) GetRecentChange() modules.ConsensusChangeID {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.RecentChange
}

func (p *persistence) SetRecentChange(rc modules.ConsensusChangeID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.RecentChange = rc
}

func (p *persistence) GetPublicKey() types.SiaPublicKey {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.PublicKey
}

func (p *persistence) SetPublicKey(pk types.SiaPublicKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.PublicKey = pk
}

func (p *persistence) GetRevisionNumber() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.RevisionNumber
}

func (p *persistence) SetRevisionNumber(rn uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.RevisionNumber = rn
}

func (p *persistence) GetSettings() modules.PoolInternalSettings {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Settings
}

func (p *persistence) SetSettings(s modules.PoolInternalSettings) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Settings = s
}

func (p *persistence) GetUnlockHash() types.UnlockHash {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.UnlockHash
}

func (p *persistence) SetUnlockHash(h types.UnlockHash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.UnlockHash = h
}

func (p *persistence) GetTarget() types.Target {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Target
}

func (p *persistence) SetTarget(t types.Target) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Target = t
}

func (p *persistence) GetBlocksFound() []types.BlockID {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.BlocksFound
}

func (p *persistence) SetBlocksFound(bf []types.BlockID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.BlocksFound = bf
}

// persistData returns a copy of the data in the Pool that will be saved to disk.
func (mp *Pool) persistData() persistence {
	return persistence{
		BlockHeight:    mp.persist.GetBlockHeight(),
		RecentChange:   mp.persist.GetRecentChange(),
		PublicKey:      mp.persist.GetPublicKey(),
		RevisionNumber: mp.persist.GetRevisionNumber(),
		Settings:       mp.persist.GetSettings(),
		UnlockHash:     mp.persist.GetUnlockHash(),
		Target:         mp.persist.GetTarget(),
		BlocksFound:    mp.persist.GetBlocksFound(),
	}
}

// establishDefaults configures the default settings for the pool, overwriting
// any existing settings.
func (mp *Pool) establishDefaults() error {
	// Configure the settings object.
	/*
		mp.persist.SetSettings(modules.PoolInternalSettings{
			PoolName:               "",
			PoolWallet:             types.UnlockHash{},
			PoolNetworkPort:        6666,
			PoolDBConnection:       "user:pass@127.0.0.1/HDCPool",
		})
		mp.newSourceBlock()
	*/
	return nil
}

// loadPersistObject will take a persist object and copy the data into the
// host.
func (mp *Pool) loadPersistObject(p *persistence) {
	// Copy over consensus tracking.
	mp.persist.SetBlockHeight(p.GetBlockHeight())
	mp.persist.SetRecentChange(p.GetRecentChange())

	mp.persist.SetPublicKey(p.GetPublicKey())
	mp.persist.SetRevisionNumber(p.GetRevisionNumber())
	mp.persist.SetSettings(p.GetSettings())
	mp.persist.SetUnlockHash(p.GetUnlockHash())
	mp.persist.SetTarget(p.GetTarget())
	mp.persist.SetBlocksFound(p.GetBlocksFound())
}

func (mp *Pool) setPoolSettings(initConfig config.MiningPoolConfig) error {
	mp.log.Debugf("setPoolSettings called\n")
	var poolWallet types.UnlockHash

	poolWallet.LoadString(initConfig.PoolWallet)
	internalSettings := modules.PoolInternalSettings{
		PoolNetworkPort:  initConfig.PoolNetworkPort,
		PoolName:         initConfig.PoolName,
		PoolID:           initConfig.PoolID,
		PoolDBConnection: initConfig.PoolDBConnection,
		PoolWallet:       poolWallet,
	}
	mp.persist.SetSettings(internalSettings)
	mp.newSourceBlock()
	return nil
}

func (mp *Pool) hasSettings() bool {
	_, err := os.Stat(filepath.Join(mp.persistDir, settingsFile))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}

// load loads the Hosts's persistent data from disk.
func (mp *Pool) load() error {

	// Load the old persistence object from disk. Simple task if the version is
	// the most recent version, but older versions need to be updated to the
	// more recent structures.
	p := new(persistence)
	err := mp.dependencies.loadFile(persistMetadata, p, filepath.Join(mp.persistDir, settingsFile))
	mp.log.Printf("Loading persistence metadata")
	if err == nil {
		// Copy in the persistence.
		mp.loadPersistObject(p)
	} else if os.IsNotExist(err) {
		mp.log.Printf("Persistence metadata not found.")
		// There is no pool.json file, set up sane defaults.
		// return mp.establishDefaults()
		return nil
	} else if err != nil {
		return err
	}

	return nil
}

// saveSync stores all of the persist data to disk and then syncs to disk.
func (mp *Pool) saveSync() error {
	return persist.SaveJSON(persistMetadata, mp.persistData(), filepath.Join(mp.persistDir, settingsFile))
}
