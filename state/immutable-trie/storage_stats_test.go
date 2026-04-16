package itrie

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb"
	ldbstorage "github.com/syndtr/goleveldb/leveldb/storage"
)

func newMemLevelDB(t *testing.T) *leveldb.DB {
	t.Helper()

	db, err := leveldb.Open(ldbstorage.NewMemStorage(), nil)
	require.NoError(t, err)

	t.Cleanup(func() { db.Close() })

	return db
}

func TestKeyCount_EmptyDB(t *testing.T) {
	t.Parallel()

	db := newMemLevelDB(t)

	count, err := KeyCount(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestKeyCount_AfterInserts(t *testing.T) {
	t.Parallel()

	db := newMemLevelDB(t)

	for i := 0; i < 50; i++ {
		key := []byte{byte(i), byte(i >> 8)}
		require.NoError(t, db.Put(key, []byte("value"), nil))
	}

	count, err := KeyCount(db)
	require.NoError(t, err)
	assert.Equal(t, int64(50), count)
}

func TestKeyCount_WithCodePrefixKeys(t *testing.T) {
	t.Parallel()

	db := newMemLevelDB(t)

	// Add 10 trie node keys (32-byte hashes)
	for i := 0; i < 10; i++ {
		key := make([]byte, 32)
		key[0] = byte(i)
		require.NoError(t, db.Put(key, []byte("node-data"), nil))
	}

	// Add 5 code keys (prefixed with "code")
	for i := 0; i < 5; i++ {
		key := make([]byte, 36)
		copy(key, codePrefix)
		key[4] = byte(i)
		require.NoError(t, db.Put(key, []byte("bytecode"), nil))
	}

	// Total should be 15
	count, err := KeyCount(db)
	require.NoError(t, err)
	assert.Equal(t, int64(15), count)

	// Code-prefix count should be 5
	codeCount, err := KeyCountWithPrefix(db, codePrefix)
	require.NoError(t, err)
	assert.Equal(t, int64(5), codeCount)
}

func TestDiskSizeBytes(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "storage_stats_test")
	require.NoError(t, err)

	t.Cleanup(func() { os.RemoveAll(dir) })

	db, err := leveldb.OpenFile(dir, nil)
	require.NoError(t, err)

	// Insert some data to create files on disk
	for i := 0; i < 100; i++ {
		key := make([]byte, 32)
		key[0] = byte(i)
		require.NoError(t, db.Put(key, make([]byte, 256), nil))
	}

	require.NoError(t, db.Close())

	size, err := DiskSizeBytes(dir)
	require.NoError(t, err)
	assert.Greater(t, size, int64(0))
}

func TestDiskSizeBytes_NonexistentPath(t *testing.T) {
	t.Parallel()

	_, err := DiskSizeBytes("/nonexistent/path/xyz")
	assert.Error(t, err)
}
