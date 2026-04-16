package itrie

import (
	"os"
	"path/filepath"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// KeyCount returns the total number of keys in the LevelDB database.
func KeyCount(db *leveldb.DB) (int64, error) {
	iter := db.NewIterator(nil, nil)
	defer iter.Release()

	var count int64
	for iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, err
	}

	return count, nil
}

// KeyCountWithPrefix returns the number of keys with a given prefix.
func KeyCountWithPrefix(db *leveldb.DB, prefix []byte) (int64, error) {
	iter := db.NewIterator(util.BytesPrefix(prefix), nil)
	defer iter.Release()

	var count int64
	for iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, err
	}

	return count, nil
}

// DiskSizeBytes returns the total size of all files in the LevelDB directory.
func DiskSizeBytes(path string) (int64, error) {
	var total int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			total += info.Size()
		}

		return nil
	})

	return total, err
}
