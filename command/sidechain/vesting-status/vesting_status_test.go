package vestingstatus

import (
	"errors"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/umbracle/ethgo"
	"github.com/umbracle/ethgo/abi"
	"github.com/umbracle/ethgo/jsonrpc"
)

// mockTxRelayer implements txrelayer.TxRelayer for testing
type mockTxRelayer struct {
	mock.Mock
}

func (m *mockTxRelayer) Call(from ethgo.Address, to ethgo.Address, input []byte) (string, error) {
	args := m.Called(from, to, input)

	return args.String(0), args.Error(1)
}

func (m *mockTxRelayer) SendTransaction(txn *ethgo.Transaction, key ethgo.Key) (*ethgo.Receipt, error) {
	args := m.Called(txn, key)

	return args.Get(0).(*ethgo.Receipt), args.Error(1) //nolint:forcetypeassert
}

func (m *mockTxRelayer) SendTransactionLocal(txn *ethgo.Transaction) (*ethgo.Receipt, error) {
	args := m.Called(txn)

	return args.Get(0).(*ethgo.Receipt), args.Error(1) //nolint:forcetypeassert
}

func (m *mockTxRelayer) Client() *jsonrpc.Client {
	args := m.Called()

	return args.Get(0).(*jsonrpc.Client) //nolint:forcetypeassert
}

var (
	testAddr = ethgo.Address{0x01, 0x02, 0x03}

	// ABI type matching the vestedStakingPositions return struct
	vestingPositionABI = abi.MustNewType(
		"tuple(uint256 duration, uint256 start, uint256 end, uint256 base, uint256 vestBonus, uint256 rsiBonus, uint256 commission)",
	)

	// ABI type for single uint256 return
	uint256ABI = abi.MustNewType("uint256")
)

// encodeVestingPosition ABI-encodes a vesting position tuple and returns a hex string
func encodeVestingPosition(t *testing.T, duration, start, end, base, vestBonus, rsiBonus, commission *big.Int) string {
	t.Helper()

	encoded, err := vestingPositionABI.Encode(map[string]interface{}{
		"duration":   duration,
		"start":      start,
		"end":        end,
		"base":       base,
		"vestBonus":  vestBonus,
		"rsiBonus":   rsiBonus,
		"commission": commission,
	})
	require.NoError(t, err)

	return hex.EncodeToHex(encoded)
}

// encodeUint256 ABI-encodes a single uint256 value and returns a hex string
func encodeUint256(t *testing.T, value *big.Int) string {
	t.Helper()

	encoded, err := uint256ABI.Encode(value)
	require.NoError(t, err)

	return hex.EncodeToHex(encoded)
}

func Test_queryVestedStakingPositions_ActivePosition(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	response := encodeVestingPosition(t,
		big.NewInt(52),         // duration
		big.NewInt(1700000000), // start
		big.NewInt(1731536000), // end
		big.NewInt(1000),       // base
		big.NewInt(500),        // vestBonus
		big.NewInt(200),        // rsiBonus
		big.NewInt(10),         // commission
	)

	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(response, nil).Once()

	result, err := queryVestedStakingPositions(relayer, testAddr)
	require.NoError(t, err)

	assert.Equal(t, big.NewInt(52), result["duration"])
	assert.Equal(t, big.NewInt(1700000000), result["start"])
	assert.Equal(t, big.NewInt(1731536000), result["end"])
	assert.Equal(t, big.NewInt(1000), result["base"])
	assert.Equal(t, big.NewInt(500), result["vestBonus"])
	assert.Equal(t, big.NewInt(200), result["rsiBonus"])
	assert.Equal(t, big.NewInt(10), result["commission"])

	relayer.AssertExpectations(t)
}

func Test_queryVestedStakingPositions_NoPosition(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	// All zeros = no vesting position
	response := encodeVestingPosition(t,
		big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
	)

	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(response, nil).Once()

	result, err := queryVestedStakingPositions(relayer, testAddr)
	require.NoError(t, err)

	start := result["start"].(*big.Int) //nolint:forcetypeassert
	assert.Equal(t, 0, start.Sign(), "start should be zero for no position")

	relayer.AssertExpectations(t)
}

func Test_queryVestedStakingPositions_CallError(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).
		Return("", errors.New("rpc connection refused")).Once()

	_, err := queryVestedStakingPositions(relayer, testAddr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rpc connection refused")

	relayer.AssertExpectations(t)
}

func Test_querySingleUint256_ValidResponse(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	expected := big.NewInt(42000)
	response := encodeUint256(t, expected)

	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(response, nil).Once()

	result, err := querySingleUint256(relayer, testAddr, unclaimedRewardsFn, ethgo.Address{})
	require.NoError(t, err)
	assert.Equal(t, expected, result)

	relayer.AssertExpectations(t)
}

func Test_querySingleUint256_ZeroResponse(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	response := encodeUint256(t, big.NewInt(0))

	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(response, nil).Once()

	result, err := querySingleUint256(relayer, testAddr, unclaimedRewardsFn, ethgo.Address{})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Sign(), "result should be zero")

	relayer.AssertExpectations(t)
}

func Test_querySingleUint256_CallError(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).
		Return("", errors.New("timeout")).Once()

	_, err := querySingleUint256(relayer, testAddr, unclaimedRewardsFn, ethgo.Address{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")

	relayer.AssertExpectations(t)
}

func Test_getVestingStatus_ActivePosition(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	// 1. vestedStakingPositions → active position
	vestingResp := encodeVestingPosition(t,
		big.NewInt(52),
		big.NewInt(1700000000),
		big.NewInt(1731536000),
		big.NewInt(1000),
		big.NewInt(500),
		big.NewInt(200),
		big.NewInt(10),
	)

	// 2. calculatePositionTotalReward → 300
	totalRewardResp := encodeUint256(t, big.NewInt(300))

	// 3. unclaimedRewards → 100
	unclaimedResp := encodeUint256(t, big.NewInt(100))

	// 4. distributedCommissions → 50
	commissionsResp := encodeUint256(t, big.NewInt(50))

	// Set up mock calls in order (each matches by the encoded input which differs per method)
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(vestingResp, nil).Once()
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(totalRewardResp, nil).Once()
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(unclaimedResp, nil).Once()
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(commissionsResp, nil).Once()

	result, err := getVestingStatus(relayer, testAddr)
	require.NoError(t, err)

	assert.True(t, result.IsActiveVestingPosition)
	assert.Equal(t, testAddr.String(), result.ValidatorAddress)
	assert.Equal(t, "52", result.VestingDuration)
	assert.Contains(t, result.VestingStart, "1700000000")
	assert.Contains(t, result.VestingEnd, "1731536000")
	assert.Equal(t, "1000", result.BaseStake)
	assert.Equal(t, "500", result.VestBonus)
	assert.Equal(t, "200", result.RSIBonus)
	assert.Equal(t, "10", result.VestingCommission)
	assert.Equal(t, "300", result.GeneratedRewards)
	assert.Equal(t, "100", result.ClaimableRewards)
	assert.Equal(t, "50", result.ClaimableCommissions)

	relayer.AssertExpectations(t)
}

func Test_getVestingStatus_InactivePosition(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	// All zeros = no vesting position
	vestingResp := encodeVestingPosition(t,
		big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
	)

	totalRewardResp := encodeUint256(t, big.NewInt(0))
	unclaimedResp := encodeUint256(t, big.NewInt(42))
	commissionsResp := encodeUint256(t, big.NewInt(7))

	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(vestingResp, nil).Once()
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(totalRewardResp, nil).Once()
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(unclaimedResp, nil).Once()
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(commissionsResp, nil).Once()

	result, err := getVestingStatus(relayer, testAddr)
	require.NoError(t, err)

	assert.False(t, result.IsActiveVestingPosition)
	assert.Equal(t, "42", result.ClaimableRewards)
	assert.Equal(t, "7", result.ClaimableCommissions)

	relayer.AssertExpectations(t)
}

func Test_getVestingStatus_VestingQueryFails(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).
		Return("", errors.New("contract reverted")).Once()

	_, err := getVestingStatus(relayer, testAddr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query vesting position")

	relayer.AssertExpectations(t)
}

func Test_getVestingStatus_TotalRewardQueryFails(t *testing.T) {
	t.Parallel()

	relayer := new(mockTxRelayer)

	vestingResp := encodeVestingPosition(t,
		big.NewInt(0), big.NewInt(0), big.NewInt(0),
		big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0),
	)

	// First call succeeds, second fails
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).Return(vestingResp, nil).Once()
	relayer.On("Call", mock.Anything, mock.Anything, mock.Anything).
		Return("", errors.New("execution reverted")).Once()

	_, err := getVestingStatus(relayer, testAddr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to query total reward")

	relayer.AssertExpectations(t)
}
