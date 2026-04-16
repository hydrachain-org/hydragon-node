package prune

import (
	"errors"
	"testing"

	"github.com/0xPolygon/polygon-edge/blockchain/storage"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLatestStateRoot_ReturnsHeaderStateRoot(t *testing.T) {
	t.Parallel()

	expectedRoot := types.StringToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	blockHash := types.StringToHash("0x1111111111111111111111111111111111111111111111111111111111111111")

	mock := storage.NewMockStorage()
	mock.HookReadHeadNumber(func() (uint64, bool) {
		return 100, true
	})
	mock.HookReadCanonicalHash(func(n uint64) (types.Hash, bool) {
		if n == 100 {
			return blockHash, true
		}

		return types.Hash{}, false
	})
	mock.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
		if hash == blockHash {
			return &types.Header{
				Number:    100,
				StateRoot: expectedRoot,
			}, nil
		}

		return nil, errors.New("not found")
	})

	root, blockNum, err := GetLatestStateRoot(mock)
	require.NoError(t, err)
	assert.Equal(t, expectedRoot, root)
	assert.Equal(t, uint64(100), blockNum)
}

func TestGetLatestStateRoot_EmptyChain(t *testing.T) {
	t.Parallel()

	mock := storage.NewMockStorage()
	mock.HookReadHeadNumber(func() (uint64, bool) {
		return 0, false
	})

	_, _, err := GetLatestStateRoot(mock)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "head")
}

func TestGetStateRootAtBlock_ReturnsCorrectRoot(t *testing.T) {
	t.Parallel()

	roots := map[uint64]types.Hash{
		5:  types.StringToHash("0x5555555555555555555555555555555555555555555555555555555555555555"),
		10: types.StringToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}

	hashes := map[uint64]types.Hash{
		5:  types.StringToHash("0x0505050505050505050505050505050505050505050505050505050505050505"),
		10: types.StringToHash("0x0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a"),
	}

	mock := storage.NewMockStorage()
	mock.HookReadCanonicalHash(func(n uint64) (types.Hash, bool) {
		h, ok := hashes[n]

		return h, ok
	})
	mock.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
		for num, h := range hashes {
			if h == hash {
				return &types.Header{
					Number:    num,
					StateRoot: roots[num],
				}, nil
			}
		}

		return nil, errors.New("not found")
	})

	root, err := GetStateRootAtBlock(mock, 5)
	require.NoError(t, err)
	assert.Equal(t, roots[5], root)

	root, err = GetStateRootAtBlock(mock, 10)
	require.NoError(t, err)
	assert.Equal(t, roots[10], root)
}

func TestGetStateRootAtBlock_NonexistentBlock(t *testing.T) {
	t.Parallel()

	mock := storage.NewMockStorage()
	mock.HookReadCanonicalHash(func(n uint64) (types.Hash, bool) {
		return types.Hash{}, false
	})

	_, err := GetStateRootAtBlock(mock, 999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "canonical hash")
}
