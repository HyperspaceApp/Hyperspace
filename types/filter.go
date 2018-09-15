package types

import (
	"github.com/HyperspaceApp/Hyperspace/gcs"
)

type GCSFilter struct {
	filter gcs.Filter
}

func NewGCSFilter(filter *gcs.Filter) GCSFilter {
	return GCSFilter{filter: *filter}
}

// MatchUnlockHash checks whether an unlockhash in a processed block
func (f GCSFilter) MatchUnlockHash(id []byte, data [][]byte) bool {
	var key [gcs.KeySize]byte
	copy(key[:], id)

	return f.filter.MatchAny(key, data)
}

func (f *GCSFilter) LoadBytes(bytes []byte) error {
	loadedFilter, err := gcs.FromNPBytes(bytes)
	f.filter = *loadedFilter
	if err != nil {
		return err
	}
	return nil
}
