package itrie

import (
	"testing"

	"math/big"

	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb"
	ldbstorage "github.com/syndtr/goleveldb/leveldb/storage"
	"pgregory.net/rapid"
)

func newMemLevelDBPair(t *testing.T) (*leveldb.DB, *leveldb.DB) {
	t.Helper()

	srcDB, err := leveldb.Open(ldbstorage.NewMemStorage(), nil)
	require.NoError(t, err)

	dstDB, err := leveldb.Open(ldbstorage.NewMemStorage(), nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		srcDB.Close()
		dstDB.Close()
	})

	return srcDB, dstDB
}

func TestPruneTrie_PropertyBased(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(tt *rapid.T) {
		srcDB, dstDB := newMemLevelDBPair(t)
		kv := NewKV(srcDB)

		state := NewState(kv)
		trie := state.newTrie()
		tx := trie.Txn(kv)

		// Insert random trie entries
		n := rapid.IntRange(1, 200).Draw(tt, "n")
		for i := 0; i < n; i++ {
			key := rapid.SliceOfN(rapid.Byte(), 32, 32).Draw(tt, "key")
			value := rapid.SliceOfN(rapid.Byte(), 10, 80).Draw(tt, "value")
			tx.Insert(key, value)
		}

		tx.Commit()
		stateRoot := trie.Hash()

		// Add garbage keys (simulating old block trie nodes)
		garbageCount := rapid.IntRange(10, 100).Draw(tt, "garbage")
		for i := 0; i < garbageCount; i++ {
			garbageKey := rapid.SliceOfN(rapid.Byte(), 32, 32).Draw(tt, "garbageKey")
			garbageVal := rapid.SliceOfN(rapid.Byte(), 20, 100).Draw(tt, "garbageVal")
			_ = srcDB.Put(garbageKey, garbageVal, nil)
		}

		srcKeys, err := KeyCount(srcDB)
		require.NoError(t, err)

		// Prune
		result, err := PruneTrie(types.BytesToHash(stateRoot.Bytes()), srcDB, dstDB)
		require.NoError(t, err)
		require.True(t, result.Validated)

		// Destination should have fewer keys than source (garbage removed)
		assert.Less(t, result.DestKeys, srcKeys)
		assert.Greater(t, result.DestKeys, int64(0))

		// HashChecker on destination should match
		checkedRoot, err := HashChecker(stateRoot.Bytes(), NewKV(dstDB))
		require.NoError(t, err)
		assert.Equal(t, types.BytesToHash(stateRoot.Bytes()), checkedRoot)
	})
}

func TestFlushingBatch_FlushesAtInterval(t *testing.T) {
	t.Parallel()

	db := newMemLevelDB(t)
	kv := NewKV(db)

	fb := newFlushingBatch(kv, 10)

	// Write 25 entries — should trigger 2 auto-flushes (at 10 and 20), leaving 5 pending
	for i := 0; i < 25; i++ {
		key := make([]byte, 32)
		key[0] = byte(i)
		fb.Put(key, []byte("value"))
	}

	// Before final Write, 20 entries should already be in DB (from 2 flushes)
	count, err := KeyCount(db)
	require.NoError(t, err)
	assert.Equal(t, int64(20), count)

	// Final Write flushes remaining 5
	require.NoError(t, fb.Write())

	count, err = KeyCount(db)
	require.NoError(t, err)
	assert.Equal(t, int64(25), count)
	assert.Equal(t, int64(25), fb.total)
}

func TestPruneTrie_WithContractCode(t *testing.T) {
	t.Parallel()

	srcDB, dstDB := newMemLevelDBPair(t)
	kv := NewKV(srcDB)

	// Store contract code directly in the source DB with the code prefix
	codeHash := types.StringToHash("0xdeadbeef00000000000000000000000000000000000000000000000000000001")
	codeBytes := []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	require.NoError(t, kv.SetCode(codeHash, codeBytes))

	// Verify code is in source
	code, ok := kv.GetCode(codeHash)
	require.True(t, ok)
	require.Equal(t, codeBytes, code)

	// CopyTrie only copies code when it encounters accounts with CodeHash in the trie.
	// For a standalone code copy test, verify PruneTrie copies code-prefix keys.
	// Since CopyTrie walks the trie and copies code it finds via account references,
	// code that isn't referenced by any account in the trie won't be copied.
	// This is correct behavior — orphaned code should be pruned too.

	// Create a minimal trie
	state := NewState(kv)
	trie := state.newTrie()
	tx := trie.Txn(kv)
	tx.Insert(make([]byte, 32), []byte("account-data"))
	tx.Commit()
	stateRoot := trie.Hash()

	result, err := PruneTrie(types.BytesToHash(stateRoot.Bytes()), srcDB, dstDB)
	require.NoError(t, err)
	require.True(t, result.Validated)

	// Unreferenced code should NOT be in destination (correctly pruned)
	dstKV := NewKV(dstDB)
	_, ok = dstKV.GetCode(codeHash)
	assert.False(t, ok, "orphaned code should be pruned")
}

func TestPruneTrie_InvalidStateRoot(t *testing.T) {
	t.Parallel()

	srcDB, dstDB := newMemLevelDBPair(t)

	badRoot := types.StringToHash("0xbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbad00")

	_, err := PruneTrie(badRoot, srcDB, dstDB)
	assert.Error(t, err)
}

func TestPruneTrie_MultipleBlocks_OnlyLatestRetained(t *testing.T) {
	t.Parallel()

	srcDB, dstDB := newMemLevelDBPair(t)
	kv := NewKV(srcDB)
	st := NewState(kv)

	// Block 1: commit accounts via Snapshot.Commit (production path, writes to LevelDB)
	snap1 := st.NewSnapshot()

	objs1 := make([]*state.Object, 5)
	for i := 0; i < 5; i++ {
		addr := types.Address{}
		addr[0] = byte(i + 1)

		objs1[i] = &state.Object{
			Address:  addr,
			CodeHash: types.EmptyCodeHash,
			Balance:  big.NewInt(int64(1000 * (i + 1))),
			Root:     types.EmptyRootHash,
			Nonce:    uint64(i),
		}
	}

	snap2, root1, err := snap1.Commit(objs1)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyRootHash.Bytes(), root1)

	keysAfterBlock1, err := KeyCount(srcDB)
	require.NoError(t, err)
	require.Greater(t, keysAfterBlock1, int64(0))

	// Block 2: modify balances → different state root, more nodes written
	objs2 := make([]*state.Object, 5)
	for i := 0; i < 5; i++ {
		addr := types.Address{}
		addr[0] = byte(i + 1)

		objs2[i] = &state.Object{
			Address:  addr,
			CodeHash: types.EmptyCodeHash,
			Balance:  big.NewInt(int64(9999 * (i + 1))),
			Root:     types.EmptyRootHash,
			Nonce:    uint64(i + 100),
		}
	}

	_, root2, err := snap2.Commit(objs2)
	require.NoError(t, err)
	require.NotEqual(t, root1, root2, "two blocks with different balances must have different roots")

	srcKeys, err := KeyCount(srcDB)
	require.NoError(t, err)
	require.Greater(t, srcKeys, keysAfterBlock1, "block 2 should add more keys")

	// Prune using block 2's root
	result, err := PruneTrie(types.BytesToHash(root2), srcDB, dstDB)
	require.NoError(t, err)
	require.True(t, result.Validated)

	// Destination should have fewer keys (block 1's orphaned nodes pruned)
	assert.Less(t, result.DestKeys, srcKeys)
	assert.Greater(t, result.DestKeys, int64(0))
}
