package wallet

import (
	//"fmt"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// lookahead keeps the unlock conditions organized in a way such that it's easy to
// query their position.
type lookahead struct {
	initialized bool
	seed modules.Seed
	startingIndex uint64
	hashIndexMap map[types.UnlockHash]uint64
	keys []spendableKey
}

func (la *lookahead) GetIndex(uh types.UnlockHash) (uint64, bool) {
	index, ok := la.hashIndexMap[uh]
	return index, ok
}

func (la *lookahead) AppendKey(key spendableKey) {
	index := la.startingIndex + uint64(len(la.keys))
	la.keys = append(la.keys, key)
	uh := key.UnlockConditions.UnlockHash()
	la.hashIndexMap[uh] = index
}

func (la *lookahead) GetKeyByIndex(index uint64) spendableKey {
	//fmt.Printf("GetKeyByIndex(%v): startingIndex: %v\n", index, la.startingIndex)
	if (index - la.startingIndex) > uint64(len(la.keys)) {
		panic("GetKeyByIndex out of bounds")
	}
	return la.keys[index - la.startingIndex]
}

func (la *lookahead) GetNextKey() spendableKey {
	return la.keys[0]
}

func (la *lookahead) PopNextKey() spendableKey {
	keys := la.PopNextKeys(1)
	return keys[0]
}

func (la *lookahead) PopNextKeys(n uint64) []spendableKey {
	keys, remaining := la.keys[:n], la.keys[n:]
	la.keys = remaining
	for _, key := range keys {
		delete(la.hashIndexMap, key.UnlockConditions.UnlockHash())
	}
	la.startingIndex += n
	return keys
}

func (la *lookahead) Length() uint64 {
	return uint64(len(la.keys))
}

func (la *lookahead) Advance(numKeys uint64) []spendableKey {
	//fmt.Printf("Advanced %v keys\n", numKeys)
	newIndex := la.startingIndex + uint64(len(la.keys))
	retKeys := []spendableKey{}

	for _, k := range generateKeys(la.seed, newIndex, numKeys) {
		retKeys = append(retKeys, la.PopNextKey())
		la.AppendKey(k)
	}
	return retKeys
}

func (la *lookahead) Initialized() bool {
	return la.initialized
}

func (la *lookahead) Initialize(seed modules.Seed, startingIndex uint64) {
	//fmt.Println("Initialize called on lookahead")
	la.seed = seed
	la.startingIndex = startingIndex
	// do the initial growing of the buffer
	for _, k := range generateKeys(la.seed, startingIndex, AddressGapLimit) {
		la.AppendKey(k)
	}
	la.initialized = true
}

func newLookahead() lookahead {
	return lookahead{
		hashIndexMap: make(map[types.UnlockHash]uint64),
		keys: make([]spendableKey, 0, AddressGapLimit),
	}
}
