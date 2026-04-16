package prune

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	chainstorage "github.com/0xPolygon/polygon-edge/blockchain/storage"
	ldbchain "github.com/0xPolygon/polygon-edge/blockchain/storage/leveldb"
	"github.com/0xPolygon/polygon-edge/state"
	itrie "github.com/0xPolygon/polygon-edge/state/immutable-trie"
	"github.com/0xPolygon/polygon-edge/types"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

func TestIntegration_FullPipeline(t *testing.T) {
	// Create temp directories
	baseDir, err := os.MkdirTemp("", "prune_integration")
	require.NoError(t, err)

	defer os.RemoveAll(baseDir)

	dataDir := filepath.Join(baseDir, "node-data")
	triePath := filepath.Join(dataDir, "trie")
	blockchainPath := filepath.Join(dataDir, "blockchain")
	targetPath := filepath.Join(baseDir, "trie_new")

	require.NoError(t, os.MkdirAll(triePath, 0755))
	require.NoError(t, os.MkdirAll(blockchainPath, 0755))

	// Create trie storage and state
	trieStorage, err := itrie.NewLevelDBStorage(triePath, hclog.NewNullLogger())
	require.NoError(t, err)

	st := itrie.NewState(trieStorage)

	// Create chain storage
	chainStorage, err := ldbchain.NewLevelDBStorage(blockchainPath, hclog.NewNullLogger())
	require.NoError(t, err)

	// Simulate multiple blocks with account state changes
	accounts := []types.Address{
		{0x01}, {0x02}, {0x03}, {0x04}, {0x05},
	}

	snap := st.NewSnapshot()
	var lastRoot []byte

	for block := uint64(1); block <= 5; block++ {
		objs := make([]*state.Object, len(accounts))
		for i, addr := range accounts {
			objs[i] = &state.Object{
				Address:  addr,
				CodeHash: types.EmptyCodeHash,
				Balance:  big.NewInt(int64(1000*block) + int64(i)),
				Root:     types.EmptyRootHash,
				Nonce:    block,
			}
		}

		var root []byte
		snap, root, err = snap.Commit(objs)
		require.NoError(t, err)
		require.NotNil(t, root)

		lastRoot = root

		// Write block header to chain storage
		header := &types.Header{
			Number:    block,
			StateRoot: types.BytesToHash(root),
			Hash:      types.BytesToHash([]byte{byte(block), 0xAA}), // deterministic hash
		}

		bw := chainstorage.NewBatchWriter(chainStorage)
		bw.PutCanonicalHeader(header, big.NewInt(int64(block)))
		require.NoError(t, bw.WriteBatch())
	}

	// Close storages before pruning (prune opens read-only)
	trieStorage.Close()
	chainStorage.Close()

	// Verify source has accumulated keys from all 5 blocks
	srcDB, err := leveldb.OpenFile(triePath, &opt.Options{ReadOnly: true})
	require.NoError(t, err)

	srcKeys, err := itrie.KeyCount(srcDB)
	require.NoError(t, err)

	srcDB.Close()

	require.Greater(t, srcKeys, int64(0), "source should have trie nodes")

	// Run prune via the programmatic interface
	srcDB, err = leveldb.OpenFile(triePath, &opt.Options{ReadOnly: true})
	require.NoError(t, err)

	dstDB, err := leveldb.OpenFile(targetPath, nil)
	require.NoError(t, err)

	result, err := itrie.PruneTrie(types.BytesToHash(lastRoot), srcDB, dstDB)
	require.NoError(t, err)
	require.True(t, result.Validated)
	assert.Less(t, result.DestKeys, srcKeys, "pruned trie should have fewer keys")
	assert.Greater(t, result.DestKeys, int64(0), "pruned trie should not be empty")

	srcDB.Close()
	dstDB.Close()

	// Verify: create state from pruned trie, read accounts
	prunedStorage, err := itrie.NewLevelDBStorage(targetPath, hclog.NewNullLogger())
	require.NoError(t, err)

	defer prunedStorage.Close()

	prunedState := itrie.NewState(prunedStorage)

	prunedSnap, err := prunedState.NewSnapshotAt(types.BytesToHash(lastRoot))
	require.NoError(t, err)

	// Verify all accounts from block 5 are readable
	for i, addr := range accounts {
		account, err := prunedSnap.GetAccount(addr)
		require.NoError(t, err, "account %s should be readable from pruned trie", addr)
		require.NotNil(t, account, "account %s should exist", addr)

		expectedBalance := big.NewInt(int64(1000*5) + int64(i))
		assert.Equal(t, expectedBalance, account.Balance, "account %s balance mismatch", addr)
		assert.Equal(t, uint64(5), account.Nonce, "account %s nonce mismatch", addr)
	}

	// Verify state root resolution works via our helper
	chainStorage2, err := ldbchain.NewLevelDBStorage(blockchainPath, hclog.NewNullLogger())
	require.NoError(t, err)

	defer chainStorage2.Close()

	resolvedRoot, resolvedBlock, err := GetLatestStateRoot(chainStorage2)
	require.NoError(t, err)
	assert.Equal(t, types.BytesToHash(lastRoot), resolvedRoot)
	assert.Equal(t, uint64(5), resolvedBlock)
}

func TestIntegration_SpecificBlock(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "prune_specific_block")
	require.NoError(t, err)

	defer os.RemoveAll(baseDir)

	dataDir := filepath.Join(baseDir, "node-data")
	triePath := filepath.Join(dataDir, "trie")
	blockchainPath := filepath.Join(dataDir, "blockchain")
	targetPath := filepath.Join(baseDir, "trie_new")

	require.NoError(t, os.MkdirAll(triePath, 0755))
	require.NoError(t, os.MkdirAll(blockchainPath, 0755))

	trieStorage, err := itrie.NewLevelDBStorage(triePath, hclog.NewNullLogger())
	require.NoError(t, err)

	st := itrie.NewState(trieStorage)
	chainStorage, err := ldbchain.NewLevelDBStorage(blockchainPath, hclog.NewNullLogger())
	require.NoError(t, err)

	snap := st.NewSnapshot()
	roots := make(map[uint64]types.Hash)

	for block := uint64(1); block <= 3; block++ {
		objs := []*state.Object{{
			Address:  types.Address{byte(block)},
			CodeHash: types.EmptyCodeHash,
			Balance:  big.NewInt(int64(block * 100)),
			Root:     types.EmptyRootHash,
			Nonce:    block,
		}}

		var root []byte
		snap, root, err = snap.Commit(objs)
		require.NoError(t, err)

		roots[block] = types.BytesToHash(root)

		header := &types.Header{
			Number:    block,
			StateRoot: types.BytesToHash(root),
			Hash:      types.BytesToHash([]byte{byte(block), 0xBB}),
		}

		bw := chainstorage.NewBatchWriter(chainStorage)
		bw.PutCanonicalHeader(header, big.NewInt(int64(block)))
		require.NoError(t, bw.WriteBatch())
	}

	trieStorage.Close()
	chainStorage.Close()

	// Prune at block 2 (not latest)
	srcDB, err := leveldb.OpenFile(triePath, &opt.Options{ReadOnly: true})
	require.NoError(t, err)

	dstDB, err := leveldb.OpenFile(targetPath, nil)
	require.NoError(t, err)

	result, err := itrie.PruneTrie(roots[2], srcDB, dstDB)
	require.NoError(t, err)
	require.True(t, result.Validated)

	srcDB.Close()
	dstDB.Close()

	// Verify block 2's state is accessible
	prunedStorage, err := itrie.NewLevelDBStorage(targetPath, hclog.NewNullLogger())
	require.NoError(t, err)

	defer prunedStorage.Close()

	prunedState := itrie.NewState(prunedStorage)
	prunedSnap, err := prunedState.NewSnapshotAt(roots[2])
	require.NoError(t, err)

	// Account from block 2 should exist
	account, err := prunedSnap.GetAccount(types.Address{0x02})
	require.NoError(t, err)
	require.NotNil(t, account)
	assert.Equal(t, big.NewInt(200), account.Balance)
}

func TestIntegration_SourceUnchanged(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "prune_source_intact")
	require.NoError(t, err)

	defer os.RemoveAll(baseDir)

	triePath := filepath.Join(baseDir, "trie")
	targetPath := filepath.Join(baseDir, "trie_new")

	trieStorage, err := itrie.NewLevelDBStorage(triePath, hclog.NewNullLogger())
	require.NoError(t, err)

	st := itrie.NewState(trieStorage)
	snap := st.NewSnapshot()

	objs := []*state.Object{{
		Address:  types.Address{0x01},
		CodeHash: types.EmptyCodeHash,
		Balance:  big.NewInt(42),
		Root:     types.EmptyRootHash,
		Nonce:    1,
	}}

	_, root, err := snap.Commit(objs)
	require.NoError(t, err)

	trieStorage.Close()

	// Count source keys before prune
	srcDB, err := leveldb.OpenFile(triePath, &opt.Options{ReadOnly: true})
	require.NoError(t, err)

	keysBefore, err := itrie.KeyCount(srcDB)
	require.NoError(t, err)

	srcDB.Close()

	// Run prune
	srcDB, err = leveldb.OpenFile(triePath, &opt.Options{ReadOnly: true})
	require.NoError(t, err)

	dstDB, err := leveldb.OpenFile(targetPath, nil)
	require.NoError(t, err)

	_, err = itrie.PruneTrie(types.BytesToHash(root), srcDB, dstDB)
	require.NoError(t, err)

	srcDB.Close()
	dstDB.Close()

	// Verify source unchanged
	srcDB, err = leveldb.OpenFile(triePath, &opt.Options{ReadOnly: true})
	require.NoError(t, err)

	keysAfter, err := itrie.KeyCount(srcDB)
	require.NoError(t, err)

	srcDB.Close()

	assert.Equal(t, keysBefore, keysAfter, "source should have identical key count after prune")

	// Verify source state root still valid
	srcStorage, err := itrie.NewLevelDBStorage(triePath, hclog.NewNullLogger())
	require.NoError(t, err)

	defer srcStorage.Close()

	checkedRoot, err := itrie.HashChecker(root, srcStorage)
	require.NoError(t, err)
	assert.Equal(t, types.BytesToHash(root), checkedRoot, "source state root should be unchanged")
}
