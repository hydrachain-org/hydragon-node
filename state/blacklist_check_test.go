package state

import (
	"errors"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bridgeAttacker is the sole entry in the build-embedded baseline blacklist
// (blacklist/baseline_blacklist.txt) — the May-2026 bridge incident wallet.
var bridgeAttacker = types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")

// newBlacklistTestTransition builds a Transition isolated to exercise
// checkAndProcessTx check #5: NonPayable skips the fee/balance checks (#2,#3),
// a fresh sender (nonce 0) satisfies the nonce check (#1), and the
// senderBlacklist fork flag is set per the test case.
func newBlacklistTestTransition(forkActive bool) *Transition {
	tr := newTestTransition(nil)
	tr.config = chain.ForksInTime{SenderBlacklist: forkActive}
	tr.ctx.NonPayable = true

	return tr
}

func blacklistTestTx(from types.Address) *types.Transaction {
	return &types.Transaction{
		From:     from,
		Nonce:    0,
		Gas:      21000,
		GasPrice: big.NewInt(0),
		Value:    big.NewInt(0),
	}
}

func TestCheckAndProcessTx_SenderBlacklist(t *testing.T) {
	t.Parallel()

	normalAddr := types.StringToAddress("0x1111111111111111111111111111111111111111")

	t.Run("fork inactive: blacklisted sender passes (backward-compatible no-op)", func(t *testing.T) {
		t.Parallel()

		err := checkAndProcessTx(blacklistTestTx(bridgeAttacker), newBlacklistTestTransition(false))
		assert.NoError(t, err,
			"before the senderBlacklist fork activates, check #5 must be a strict no-op — even for the attacker")
	})

	t.Run("fork active: blacklisted sender rejected with the consensus error", func(t *testing.T) {
		t.Parallel()

		err := checkAndProcessTx(blacklistTestTx(bridgeAttacker), newBlacklistTestTransition(true))
		require.Error(t, err, "post-activation a tx from the baseline-blacklisted sender must be rejected")

		var appErr *TransitionApplicationError
		require.True(t, errors.As(err, &appErr),
			"must surface as *TransitionApplicationError so ProcessBlock fails the whole block")
		assert.ErrorIs(t, appErr.Err, ErrConsensusBlacklistedSender)
		assert.True(t, appErr.IsRecoverable,
			"check #5 is recoverable=true, consistent with checks #1-4: a proposer skips the tx, "+
				"a verifier still rejects the block (ProcessBlock ignores the flag)")
	})

	t.Run("fork active: non-blacklisted sender passes", func(t *testing.T) {
		t.Parallel()

		err := checkAndProcessTx(blacklistTestTx(normalAddr), newBlacklistTestTransition(true))
		assert.NoError(t, err, "the fork must not affect a non-blacklisted sender")
	})

	t.Run("fork inactive: non-blacklisted sender passes", func(t *testing.T) {
		t.Parallel()

		err := checkAndProcessTx(blacklistTestTx(normalAddr), newBlacklistTestTransition(false))
		assert.NoError(t, err)
	})

	// Address-normalization (guardian MEDIUM): the baseline file stores the
	// attacker address lowercased; a real sender is recovered as a
	// types.Address ([20]byte) independent of source casing. Constructing the
	// sender from a MIXED-CASE hex string must still match the baseline entry.
	t.Run("fork active: mixed-case attacker address still matches the baseline", func(t *testing.T) {
		t.Parallel()

		mixedCase := types.StringToAddress("0xD06e82E2ACD26848f86D0f559F7037CD8896071b")
		require.Equal(t, bridgeAttacker, mixedCase, "sanity: StringToAddress must be case-insensitive")

		err := checkAndProcessTx(blacklistTestTx(mixedCase), newBlacklistTestTransition(true))
		require.Error(t, err)

		var appErr *TransitionApplicationError
		require.True(t, errors.As(err, &appErr))
		assert.ErrorIs(t, appErr.Err, ErrConsensusBlacklistedSender)
	})

	// The system caller is caught by check #4 and must never reach check #5 —
	// consensus state transactions originate from SystemCaller and must never
	// be blacklist-rejected. This guards against a future regression that
	// mirrors check #5 into checkAndProcessStateTx.
	t.Run("fork active: system caller is rejected by check #4, never by the blacklist check", func(t *testing.T) {
		t.Parallel()

		err := checkAndProcessTx(blacklistTestTx(contracts.SystemCaller), newBlacklistTestTransition(true))
		require.Error(t, err)

		var appErr *TransitionApplicationError
		require.True(t, errors.As(err, &appErr))
		assert.NotErrorIs(t, appErr.Err, ErrConsensusBlacklistedSender,
			"the system caller must be caught by check #4 (SystemCaller), not the blacklist check")
	})
}
