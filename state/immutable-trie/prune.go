package itrie

import (
	"fmt"
	"time"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/syndtr/goleveldb/leveldb"
)

// PruneResult holds the outcome of a trie pruning operation.
type PruneResult struct {
	StateRoot  types.Hash
	BlockNum   uint64
	SourceKeys int64
	DestKeys   int64
	SourceSize int64
	DestSize   int64
	Duration   time.Duration
	Validated  bool
}

// PruneTrie copies all trie nodes reachable from stateRoot in srcDB to dstDB,
// then validates the copy by running HashChecker. Source DB is not modified.
func PruneTrie(stateRoot types.Hash, srcDB *leveldb.DB, dstDB *leveldb.DB) (*PruneResult, error) {
	start := time.Now()

	srcKV := NewKV(srcDB)
	dstKV := NewKV(dstDB)

	// Copy all reachable nodes from the state root
	if err := CopyTrie(stateRoot.Bytes(), srcKV, dstKV, nil, false); err != nil {
		return nil, fmt.Errorf("CopyTrie failed: %w", err)
	}

	// Validate the copy produces the same state root
	checkedRoot, err := HashChecker(stateRoot.Bytes(), dstKV)
	if err != nil {
		return nil, fmt.Errorf("HashChecker failed on destination: %w", err)
	}

	if checkedRoot != stateRoot {
		return nil, fmt.Errorf("state root mismatch after copy: expected %s, got %s", stateRoot, checkedRoot)
	}

	// Collect metrics
	srcKeys, _ := KeyCount(srcDB)
	dstKeys, _ := KeyCount(dstDB)

	return &PruneResult{
		StateRoot:  stateRoot,
		SourceKeys: srcKeys,
		DestKeys:   dstKeys,
		Duration:   time.Since(start),
		Validated:  true,
	}, nil
}
