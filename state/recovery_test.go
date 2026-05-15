package state

import (
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/blacklist"
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recoveryTestActivationBlock is the fork block we pin in test fixtures.
const recoveryTestActivationBlock uint64 = 100

// recoveryTestSrcs / recoveryTestDst mirror the production constants so
// fixture preState alignment is one-to-one with what production will hit.
var (
	recoveryTestSrcAttacker = blacklist.RecoverySources[0]
	recoveryTestSrcProbe    = blacklist.RecoverySources[1]
	recoveryTestDst         = blacklist.RecoveryDestination
)

// newRecoveryTestTransition wires a Transition with the senderBlacklist fork
// active at recoveryTestActivationBlock, and a pre-state map seeded by the
// caller. The Transition uses the same in-memory Txn fixture (newTestTxn)
// used by the rest of state's tests.
func newRecoveryTestTransition(
	preState map[types.Address]*PreState,
	forkActive bool,
	forkBlock uint64,
) *Transition {
	tr := newTestTransition(preState)
	tr.logger = hclog.NewNullLogger()
	tr.config = chain.ForksInTime{
		SenderBlacklist:      forkActive,
		SenderBlacklistBlock: forkBlock,
	}

	return tr
}

// TestErc20BalanceSlot pins the storage-slot derivation for the OZ-standard
// _balances mapping. The expected value below is what eth_getStorageAt
// returned on mainnet for the attacker's hyUSD balance and matches
// balanceOf() exactly — verifying the encoding matches Solidity's
// keccak256(abi.encode(addr, uint256(0))).
func TestErc20BalanceSlot(t *testing.T) {
	t.Parallel()

	addr := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")
	got := erc20BalanceSlot(addr)
	// keccak256(left-pad-12-zeros(addr) || 32-zero-bytes), produced from mainnet
	want := types.StringToHash("0xb8e6f691d9fd5aa21f6a67eec72a5408fb5fde73b89e584ed5fb469828699ac6")

	assert.Equal(t, want, got,
		"erc20BalanceSlot must match keccak256(pad(addr,32) || pad(slot=0,32)); "+
			"a regression here breaks the recovery's ERC20 storage mutation")

	// sanity: a different address must produce a different slot
	other := types.StringToAddress("0x1111111111111111111111111111111111111111")
	assert.NotEqual(t, got, erc20BalanceSlot(other),
		"erc20BalanceSlot must be address-distinguishing")
}

// TestBigIntToHash exercises the encoding helper for storage writes.
func TestBigIntToHash(t *testing.T) {
	t.Parallel()

	t.Run("zero", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, types.Hash{}, bigIntToHash(big.NewInt(0)))
	})

	t.Run("small positive", func(t *testing.T) {
		t.Parallel()
		h := bigIntToHash(big.NewInt(1))
		assert.Equal(t, byte(0x01), h[31])
		for i := 0; i < 31; i++ {
			assert.Equal(t, byte(0x00), h[i], "left-pad")
		}
	})

	t.Run("uint256-max minus one round-trips via Bytes", func(t *testing.T) {
		t.Parallel()

		bigVal, _ := new(big.Int).SetString(
			"115792089237316195423570985008687907853269984665640564039457584007913129639934",
			10,
		)
		h := bigIntToHash(bigVal)
		recovered := new(big.Int).SetBytes(h.Bytes())
		assert.Equal(t, bigVal.String(), recovered.String())
	})
}

// TestApplyOneShotRecovery_ForkInactive_NoOp covers the strict guard: even at
// the configured activation block, if t.config.SenderBlacklist is false the
// delta must NOT fire. This protects chains that ship the patched binary but
// haven't yet added the activation block to their genesis.
func TestApplyOneShotRecovery_ForkInactive_NoOp(t *testing.T) {
	t.Parallel()

	src := recoveryTestSrcAttacker
	dst := recoveryTestDst
	pre := map[types.Address]*PreState{
		src: {Balance: 1_000_000, Nonce: 0, State: map[types.Hash]types.Hash{}},
		dst: {Balance: 50, Nonce: 0, State: map[types.Hash]types.Hash{}},
	}

	tr := newRecoveryTestTransition(pre, false, recoveryTestActivationBlock)
	ApplyOneShotRecovery(tr, recoveryTestActivationBlock)

	assert.Equal(t, uint64(1_000_000), tr.state.GetBalance(src).Uint64(),
		"source untouched when SenderBlacklist=false")
	assert.Equal(t, uint64(50), tr.state.GetBalance(dst).Uint64(),
		"destination untouched when SenderBlacklist=false")
}

// TestApplyOneShotRecovery_WrongBlock_NoOp verifies the activation-block
// guard. The delta must only fire at exactly the configured block.
func TestApplyOneShotRecovery_WrongBlock_NoOp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		blockNumber uint64
	}{
		{"one block before activation", recoveryTestActivationBlock - 1},
		{"one block after activation", recoveryTestActivationBlock + 1},
		{"genesis", 0},
		{"far past activation", recoveryTestActivationBlock + 1_000_000},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := recoveryTestSrcAttacker
			dst := recoveryTestDst
			pre := map[types.Address]*PreState{
				src: {Balance: 1_000_000, Nonce: 0, State: map[types.Hash]types.Hash{}},
				dst: {Balance: 50, Nonce: 0, State: map[types.Hash]types.Hash{}},
			}

			tr := newRecoveryTestTransition(pre, true, recoveryTestActivationBlock)
			ApplyOneShotRecovery(tr, tc.blockNumber)

			assert.Equal(t, uint64(1_000_000), tr.state.GetBalance(src).Uint64(),
				"source untouched at non-activation block")
			assert.Equal(t, uint64(50), tr.state.GetBalance(dst).Uint64(),
				"destination untouched at non-activation block")
		})
	}
}

// TestApplyOneShotRecovery_Native_TransfersToDAO is the headline native-HYDRA
// test: at activation, both source EOAs (attacker + probe) are zeroed and
// the destination's balance increases by exact sum. Supply conserved.
func TestApplyOneShotRecovery_Native_TransfersToDAO(t *testing.T) {
	t.Parallel()

	srcA := recoveryTestSrcAttacker
	srcP := recoveryTestSrcProbe
	dst := recoveryTestDst

	const (
		attackerBal uint64 = 770_000_000 // arbitrary fixture units
		probeBal    uint64 = 5_000
		dstStart    uint64 = 1_700_000_000
	)

	pre := map[types.Address]*PreState{
		srcA: {Balance: attackerBal, Nonce: 0, State: map[types.Hash]types.Hash{}},
		srcP: {Balance: probeBal, Nonce: 0, State: map[types.Hash]types.Hash{}},
		dst:  {Balance: dstStart, Nonce: 0, State: map[types.Hash]types.Hash{}},
	}

	tr := newRecoveryTestTransition(pre, true, recoveryTestActivationBlock)
	preSupplyA := tr.state.GetBalance(srcA).Uint64()
	preSupplyP := tr.state.GetBalance(srcP).Uint64()
	preSupplyD := tr.state.GetBalance(dst).Uint64()
	preTotal := preSupplyA + preSupplyP + preSupplyD

	ApplyOneShotRecovery(tr, recoveryTestActivationBlock)

	postA := tr.state.GetBalance(srcA).Uint64()
	postP := tr.state.GetBalance(srcP).Uint64()
	postD := tr.state.GetBalance(dst).Uint64()

	assert.Equal(t, uint64(0), postA, "attacker EOA native balance must be zero post-delta")
	assert.Equal(t, uint64(0), postP, "probe EOA native balance must be zero post-delta")
	assert.Equal(t, dstStart+attackerBal+probeBal, postD,
		"DAO Safe must receive the exact sum of sources' native balances")
	assert.Equal(t, preTotal, postA+postP+postD,
		"native supply across the three accounts must be conserved (transfer, not burn)")
}

// TestApplyOneShotRecovery_Idempotent verifies that re-running the delta on
// already-zeroed sources does nothing — i.e. a reorg or replay of the
// activation block doesn't double-credit the DAO.
func TestApplyOneShotRecovery_Idempotent(t *testing.T) {
	t.Parallel()

	srcA := recoveryTestSrcAttacker
	srcP := recoveryTestSrcProbe
	dst := recoveryTestDst

	pre := map[types.Address]*PreState{
		srcA: {Balance: 1_000_000, Nonce: 0, State: map[types.Hash]types.Hash{}},
		srcP: {Balance: 200, Nonce: 0, State: map[types.Hash]types.Hash{}},
		dst:  {Balance: 0, Nonce: 0, State: map[types.Hash]types.Hash{}},
	}

	tr := newRecoveryTestTransition(pre, true, recoveryTestActivationBlock)

	// first execution: drains
	ApplyOneShotRecovery(tr, recoveryTestActivationBlock)
	require.Equal(t, uint64(0), tr.state.GetBalance(srcA).Uint64())
	require.Equal(t, uint64(0), tr.state.GetBalance(srcP).Uint64())
	require.Equal(t, uint64(1_000_200), tr.state.GetBalance(dst).Uint64())

	// re-execution: idempotent (sources already zero -> no-op)
	ApplyOneShotRecovery(tr, recoveryTestActivationBlock)
	assert.Equal(t, uint64(0), tr.state.GetBalance(srcA).Uint64())
	assert.Equal(t, uint64(0), tr.state.GetBalance(srcP).Uint64())
	assert.Equal(t, uint64(1_000_200), tr.state.GetBalance(dst).Uint64(),
		"DAO balance must not double after re-running the delta")
}

// TestApplyOneShotRecovery_NonSource_Untouched verifies the delta only
// drains the addresses in blacklist.RecoverySources — every other account
// in the world must be unaffected.
func TestApplyOneShotRecovery_NonSource_Untouched(t *testing.T) {
	t.Parallel()

	bystander := types.StringToAddress("0x9999999999999999999999999999999999999999")
	srcA := recoveryTestSrcAttacker
	dst := recoveryTestDst

	pre := map[types.Address]*PreState{
		srcA:      {Balance: 1_000_000, Nonce: 0, State: map[types.Hash]types.Hash{}},
		dst:       {Balance: 0, Nonce: 0, State: map[types.Hash]types.Hash{}},
		bystander: {Balance: 42_000, Nonce: 7, State: map[types.Hash]types.Hash{}},
	}

	tr := newRecoveryTestTransition(pre, true, recoveryTestActivationBlock)
	ApplyOneShotRecovery(tr, recoveryTestActivationBlock)

	assert.Equal(t, uint64(42_000), tr.state.GetBalance(bystander).Uint64(),
		"non-source account must not be touched by the delta")
	assert.Equal(t, uint64(7), tr.state.GetNonce(bystander),
		"non-source nonce must not be touched")
}

// TestApplyOneShotRecovery_ERC20_TransfersToDAO is the headline ERC20 test:
// at activation, for each of the configured token contracts, the source's
// _balances slot is zeroed and the destination's slot increased by the
// exact same amount. Total ERC20 storage state is conserved within the
// contract — i.e. we do not touch totalSupply (slot 2).
func TestApplyOneShotRecovery_ERC20_TransfersToDAO(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, blacklist.RecoveryERC20Targets,
		"recovery ERC20 target list must be non-empty for this test to be meaningful")

	srcA := recoveryTestSrcAttacker
	srcP := recoveryTestSrcProbe
	dst := recoveryTestDst

	// Build preState: for every token, set _balances[srcA] = 900e18, _balances[srcP] = 1e18,
	// _balances[dst] = 0; set _totalSupply (slot 2) to attackerBal+probeBal so the
	// conservation check has a concrete invariant to verify.
	srcASlot := erc20BalanceSlot(srcA)
	srcPSlot := erc20BalanceSlot(srcP)
	dstSlot := erc20BalanceSlot(dst)
	totalSupplySlot := types.Hash{31: 0x02}

	attackerERC20 := new(big.Int).Mul(big.NewInt(900), big.NewInt(1_000_000_000_000_000_000))
	probeERC20 := big.NewInt(1_000_000_000_000_000_000) // 1 * 1e18

	expectedTotalSupply := new(big.Int).Add(attackerERC20, probeERC20)

	pre := map[types.Address]*PreState{
		srcA: {Balance: 0, Nonce: 0, State: map[types.Hash]types.Hash{}},
		srcP: {Balance: 0, Nonce: 0, State: map[types.Hash]types.Hash{}},
		dst:  {Balance: 0, Nonce: 0, State: map[types.Hash]types.Hash{}},
	}
	for _, token := range blacklist.RecoveryERC20Targets {
		pre[token] = &PreState{
			Balance: 0, Nonce: 0,
			State: map[types.Hash]types.Hash{
				srcASlot:        bigIntToHash(attackerERC20),
				srcPSlot:        bigIntToHash(probeERC20),
				dstSlot:         {},
				totalSupplySlot: bigIntToHash(expectedTotalSupply),
			},
		}
	}

	tr := newRecoveryTestTransition(pre, true, recoveryTestActivationBlock)
	ApplyOneShotRecovery(tr, recoveryTestActivationBlock)

	for _, token := range blacklist.RecoveryERC20Targets {
		gotSrcA := new(big.Int).SetBytes(tr.state.GetState(token, srcASlot).Bytes())
		gotSrcP := new(big.Int).SetBytes(tr.state.GetState(token, srcPSlot).Bytes())
		gotDst := new(big.Int).SetBytes(tr.state.GetState(token, dstSlot).Bytes())
		gotSupply := new(big.Int).SetBytes(tr.state.GetState(token, totalSupplySlot).Bytes())

		assert.Equal(t, "0", gotSrcA.String(),
			"token %s: attacker _balances slot must be zero post-delta", token)
		assert.Equal(t, "0", gotSrcP.String(),
			"token %s: probe _balances slot must be zero post-delta", token)
		assert.Equal(t, expectedTotalSupply.String(), gotDst.String(),
			"token %s: DAO _balances slot must receive exact sum of sources", token)
		assert.Equal(t, expectedTotalSupply.String(), gotSupply.String(),
			"token %s: totalSupply (slot 2) must be UNCHANGED — transfer within contract conserves supply", token)
	}
}

// TestApplyOneShotRecovery_ERC20_ZeroBalance_NoOp ensures the delta handles
// the case where one of the sources holds zero of a particular token (which
// is the realistic mainnet case for the probe wallet across most ERC20s).
func TestApplyOneShotRecovery_ERC20_ZeroBalance_NoOp(t *testing.T) {
	t.Parallel()

	srcA := recoveryTestSrcAttacker
	srcP := recoveryTestSrcProbe
	dst := recoveryTestDst

	srcASlot := erc20BalanceSlot(srcA)
	srcPSlot := erc20BalanceSlot(srcP)
	dstSlot := erc20BalanceSlot(dst)

	// only attacker holds balance; probe is zero; DAO has pre-existing balance
	attackerBal := big.NewInt(500)
	dstExisting := big.NewInt(100)

	token := blacklist.RecoveryERC20Targets[0]
	pre := map[types.Address]*PreState{
		srcA: {Balance: 0, State: map[types.Hash]types.Hash{}},
		srcP: {Balance: 0, State: map[types.Hash]types.Hash{}},
		dst:  {Balance: 0, State: map[types.Hash]types.Hash{}},
		token: {
			Balance: 0,
			State: map[types.Hash]types.Hash{
				srcASlot: bigIntToHash(attackerBal),
				srcPSlot: {}, // probe holds zero
				dstSlot:  bigIntToHash(dstExisting),
			},
		},
	}

	tr := newRecoveryTestTransition(pre, true, recoveryTestActivationBlock)
	ApplyOneShotRecovery(tr, recoveryTestActivationBlock)

	gotSrcA := new(big.Int).SetBytes(tr.state.GetState(token, srcASlot).Bytes())
	gotDst := new(big.Int).SetBytes(tr.state.GetState(token, dstSlot).Bytes())
	expected := new(big.Int).Add(attackerBal, dstExisting)

	assert.Equal(t, "0", gotSrcA.String(), "attacker slot zeroed")
	assert.Equal(t, expected.String(), gotDst.String(),
		"DAO slot receives only the attacker's amount (probe zero)")
}

// TestRecoveryConsensusParameters pins the destination address and source
// list at consensus-parameter level — any drift fails CI loudly, same
// pattern as the baseline_test guard.
func TestRecoveryConsensusParameters(t *testing.T) {
	t.Parallel()

	expectedDest := types.StringToAddress("0x1B4A1b89cEfBa22a8B7D6469Ef52b9fd20f8FC04")
	expectedSrcs := map[types.Address]struct{}{
		types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b"): {},
		types.StringToAddress("0xcf4d5bf9ea8cc3cf083c9ee88305c5cce87947eb"): {},
	}
	expectedERC20 := map[types.Address]struct{}{
		types.StringToAddress("0x71f16bCb805566F322a0904885D6147a76C249C5"): {},
		types.StringToAddress("0xb8043294eFf43bcD01BD33968c7ae9dbc6A4BF8B"): {},
		types.StringToAddress("0x7b29E92ba491CD0AAcb64A363af2AfC2931Fd9F4"): {},
		types.StringToAddress("0xbBf6f2d2D462185dF545c744974b7Eb6ddadFcfd"): {},
		types.StringToAddress("0xD185683dfb59e26E269e00CE1DE75942c3d5fCD2"): {},
		types.StringToAddress("0xDf382759Ea2393a26d2F71924A4f69962ff0C1c5"): {},
		types.StringToAddress("0x8C3814babbc5DAdCcee2b1b448B7a037EC4fc6f2"): {},
		types.StringToAddress("0x18FFd46709F9EBc7c686FBdD8E2A7531Ce34869F"): {},
	}

	assert.Equal(t, expectedDest, blacklist.RecoveryDestination,
		"RecoveryDestination changed — this is a CONSENSUS-PARAMETER change")

	require.Equal(t, len(expectedSrcs), len(blacklist.RecoverySources),
		"RecoverySources length changed — consensus-parameter change")
	for _, src := range blacklist.RecoverySources {
		assert.Contains(t, expectedSrcs, src,
			"unexpected entry in RecoverySources — consensus-parameter change: %s", src)
	}

	require.Equal(t, len(expectedERC20), len(blacklist.RecoveryERC20Targets),
		"RecoveryERC20Targets length changed — consensus-parameter change")
	for _, tok := range blacklist.RecoveryERC20Targets {
		assert.Contains(t, expectedERC20, tok,
			"unexpected entry in RecoveryERC20Targets — consensus-parameter change: %s", tok)
	}

	// Sanity: the storage slot constant must remain 0 (OZ-standard) — verified
	// against on-chain storage for all 8 contracts at design time. If this
	// ever changes, the on-chain layout must be re-verified per token.
	assert.Equal(t, uint64(0), blacklist.ERC20BalancesSlot,
		"ERC20BalancesSlot changed — re-verify on-chain layout for every "+
			"RecoveryERC20Targets contract before merging")

	// Sanity: keccak should hash to known value (avoids package-level break).
	check := crypto.Keccak256(make([]byte, 64))
	require.Len(t, check, 32)
}
