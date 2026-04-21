package prune

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/blockchain/storage"
	"github.com/0xPolygon/polygon-edge/types"
)

// GetLatestStateRoot reads the head block number from chain storage,
// then returns the state root from that block's header.
func GetLatestStateRoot(st storage.Storage) (types.Hash, uint64, error) {
	headNum, ok := st.ReadHeadNumber()
	if !ok {
		return types.Hash{}, 0, fmt.Errorf("failed to read head number from chain storage")
	}

	root, err := GetStateRootAtBlock(st, headNum)
	if err != nil {
		return types.Hash{}, 0, fmt.Errorf("failed to get state root at head block %d: %w", headNum, err)
	}

	return root, headNum, nil
}

// GetStateRootAtBlock returns the state root from the header at a specific block number.
func GetStateRootAtBlock(st storage.Storage, blockNum uint64) (types.Hash, error) {
	canonicalHash, ok := st.ReadCanonicalHash(blockNum)
	if !ok {
		return types.Hash{}, fmt.Errorf("failed to read canonical hash for block %d", blockNum)
	}

	header, err := st.ReadHeader(canonicalHash)
	if err != nil {
		return types.Hash{}, fmt.Errorf("failed to read header for block %d (hash %s): %w", blockNum, canonicalHash, err)
	}

	return header.StateRoot, nil
}
