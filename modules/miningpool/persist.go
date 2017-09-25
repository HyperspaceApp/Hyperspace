package pool

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/NebulousLabs/Sia/build"
	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/persist"
	"github.com/NebulousLabs/Sia/types"

	"github.com/NebulousLabs/bolt"
)

// persistence is the data that is kept when the pool is restarted.
type persistence struct {
	mu sync.RWMutex

	// Consensus Tracking.
	BlockHeight  types.BlockHeight         `json:"blockheight"`
	RecentChange modules.ConsensusChangeID `json:"recentchange"`

	// Pool Identity.
	Announced      bool                         `json:"announced"`
	AutoAddress    modules.NetAddress           `json:"autoaddress"`
	MiningMetrics  modules.PoolMiningMetrics    `json:"miningmetrics"`
	PublicKey      types.SiaPublicKey           `json:"publickey"`
	RevisionNumber uint64                       `json:"revisionnumber"`
	Settings       modules.PoolInternalSettings `json:"settings"`
	UnlockHash     types.UnlockHash             `json:"unlockhash"`

	// Block info
	Target types.Target `json:"blocktarget"`
	// Address       types.UnlockHash `json:"pooladdress"`
	BlocksFound   []types.BlockID `json:"blocksfound"`
	UnsolvedBlock types.Block     `json:"unsolvedblock"`
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

func (p *persistence) GetAnnounced() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Announced
}

func (p *persistence) SetAnnounced(an bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Announced = an
}

func (p *persistence) GetAutoAddress() modules.NetAddress {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.AutoAddress
}

func (p *persistence) SetAutoAddress(aa modules.NetAddress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.AutoAddress = aa
}

func (p *persistence) GetMiningMetrics() modules.PoolMiningMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.MiningMetrics
}

func (p *persistence) SetMiningMetrics(mm modules.PoolMiningMetrics) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.MiningMetrics = mm
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

func (p *persistence) GetCopyUnsolvedBlock() types.Block {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.UnsolvedBlock
}

// TODO: don't like this approach - needs to be resolved differently
func (p *persistence) GetUnsolvedBlockPtr() *types.Block {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return &p.UnsolvedBlock
}

func (p *persistence) SetUnsolvedBlock(ub types.Block) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.UnsolvedBlock = ub
}

// persistData returns a copy of the data in the Pool that will be saved to disk.
func (mp *Pool) persistData() persistence {
	return persistence{
		BlockHeight:    mp.persist.GetBlockHeight(),
		RecentChange:   mp.persist.GetRecentChange(),
		Announced:      mp.persist.GetAnnounced(),
		AutoAddress:    mp.persist.GetAutoAddress(),
		MiningMetrics:  mp.persist.GetMiningMetrics(),
		PublicKey:      mp.persist.GetPublicKey(),
		RevisionNumber: mp.persist.GetRevisionNumber(),
		Settings:       mp.persist.GetSettings(),
		UnlockHash:     mp.persist.GetUnlockHash(),
		Target:         mp.persist.GetTarget(),
		BlocksFound:    mp.persist.GetBlocksFound(),
		UnsolvedBlock:  mp.persist.GetCopyUnsolvedBlock(),
	}
}

// establishDefaults configures the default settings for the pool, overwriting
// any existing settings.
func (mp *Pool) establishDefaults() error {
	// Configure the settings object.
	mp.persist.SetSettings(modules.PoolInternalSettings{
		PoolName:               "",
		AcceptingShares:        false,
		PoolOperatorPercentage: 0.0,
		PoolOperatorWallet:     types.UnlockHash{},
		PoolNetworkPort:        3333,
	})
	mp.newSourceBlock()
	return nil
}

// loadPersistObject will take a persist object and copy the data into the
// host.
func (mp *Pool) loadPersistObject(p *persistence) {
	// Copy over consensus tracking.
	mp.persist.SetBlockHeight(p.GetBlockHeight())
	mp.persist.SetRecentChange(p.GetRecentChange())

	// Copy over host identity.
	mp.persist.SetAnnounced(p.GetAnnounced())
	mp.persist.SetAutoAddress(p.GetAutoAddress())
	if err := p.GetAutoAddress().IsValid(); err != nil {
		mp.log.Printf("WARN: AutoAddress '%v' loaded from persist is invalid: %v", p.AutoAddress, err)
		p.SetAutoAddress("")
	}
	mp.persist.SetMiningMetrics(p.GetMiningMetrics())
	mp.persist.SetPublicKey(p.GetPublicKey())
	mp.persist.SetRevisionNumber(p.GetRevisionNumber())
	mp.persist.SetSettings(p.GetSettings())
	mp.persist.SetUnlockHash(p.GetUnlockHash())
	mp.persist.SetTarget(p.GetTarget())
	mp.persist.SetBlocksFound(p.GetBlocksFound())
	mp.persist.SetUnsolvedBlock(p.GetCopyUnsolvedBlock())
}

// initDB will check that the database has been initialized and if not, will
// initialize the database.
func (mp *Pool) initDB() (err error) {
	// Open the pool's database and set up the stop function to close it.
	mp.db, err = mp.dependencies.openDatabase(dbMetadata, filepath.Join(mp.persistDir, dbFilename))
	if err != nil {
		return err
	}
	mp.tg.AfterStop(func() {
		err = mp.db.Close()
		if err != nil {
			mp.log.Println("Could not close the database:", err)
		}
	})

	return mp.db.Update(func(tx *bolt.Tx) error {
		// The storage obligation bucket does not exist, which means the
		// database needs to be initialized. Create the database buckets.
		buckets := [][]byte{
			bucketActionItems,
			bucketStorageObligations,
		}
		for _, bucket := range buckets {
			_, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// load loads the Hosts's persistent data from disk.
func (mp *Pool) load() error {
	// Initialize the host database.
	err := mp.initDB()
	if err != nil {
		err = build.ExtendErr("Could not initialize database:", err)
		mp.log.Println(err)
		return err
	}

	// Load the old persistence object from disk. Simple task if the version is
	// the most recent version, but older versions need to be updated to the
	// more recent structures.
	p := new(persistence)
	err = mp.dependencies.loadFile(persistMetadata, p, filepath.Join(mp.persistDir, settingsFile))
	if err == nil {
		// Copy in the persistence.
		mp.loadPersistObject(p)
	} else if os.IsNotExist(err) {
		// There is no pool.json file, set up sane defaults.
		return mp.establishDefaults()
	} else if err != nil {
		return err
	}

	return nil
}

// saveSync stores all of the persist data to disk and then syncs to disk.
func (mp *Pool) saveSync() error {
	return persist.SaveJSON(persistMetadata, mp.persistData(), filepath.Join(mp.persistDir, settingsFile))
}
