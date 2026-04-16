package itrie

import (
	"fmt"
	"time"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/syndtr/goleveldb/leveldb"
)

const defaultFlushInterval = 50000

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

// flushingBatch wraps a Storage and flushes to LevelDB every flushInterval puts,
// preventing unbounded memory growth when copying large tries.
type flushingBatch struct {
	storage       Storage
	batch         Batch
	count         int
	total         int64
	flushInterval int
}

func newFlushingBatch(storage Storage, flushInterval int) *flushingBatch {
	return &flushingBatch{
		storage:       storage,
		batch:         storage.Batch(),
		flushInterval: flushInterval,
	}
}

func (f *flushingBatch) Put(k, v []byte) {
	f.batch.Put(k, v)
	f.count++
	f.total++

	if f.count >= f.flushInterval {
		// Flush is best-effort during traversal; final flush is checked
		f.batch.Write() //nolint:errcheck
		f.batch = f.storage.Batch()
		f.count = 0
	}
}

func (f *flushingBatch) Write() error {
	if f.count > 0 {
		return f.batch.Write()
	}

	return nil
}

// CopyTrieStreaming is like CopyTrie but flushes to disk every flushInterval nodes,
// keeping memory usage bounded regardless of trie size.
func CopyTrieStreaming(nodeHash []byte, storage Storage, newStorage Storage, flushInterval int) error {
	fb := newFlushingBatch(newStorage, flushInterval)

	if err := copyTrieHash(nodeHash, storage, fb, nil, false); err != nil {
		return err
	}

	return fb.Write()
}

// PruneTrie copies all trie nodes reachable from stateRoot in srcDB to dstDB,
// then validates the copy by running HashChecker. Source DB is not modified.
// Uses streaming writes to keep memory bounded on large tries.
func PruneTrie(stateRoot types.Hash, srcDB *leveldb.DB, dstDB *leveldb.DB) (*PruneResult, error) {
	start := time.Now()

	srcKV := NewKV(srcDB)
	dstKV := NewKV(dstDB)

	// Copy all reachable nodes, flushing every 50k entries to keep memory bounded
	if err := CopyTrieStreaming(stateRoot.Bytes(), srcKV, dstKV, defaultFlushInterval); err != nil {
		return nil, fmt.Errorf("CopyTrieStreaming failed: %w", err)
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
