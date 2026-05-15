package blacklist

import (
	"strings"
	"testing"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
)

// bridgeAttacker is the May-2026 bridge incident wallet — the original entry
// in the build-embedded baseline.
var bridgeAttacker = types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")

// blacklistProbe is an operator-controlled MetaMask wallet whose sole
// purpose is to fire an end-to-end verification tx on every network after
// a senderBlacklist fork rollout — without it, post-activation behaviour
// on mainnet/testnet is unobservable because we have no key for the
// attacker address. Removing this entry silently disables the deploy
// smoke test.
var blacklistProbe = types.StringToAddress("0xcf4d5bf9ea8cc3cf083c9ee88305c5cce87947eb")

// TestBaselineSet_Deterministic pins the parsed embedded baseline to an
// exact expected set.
//
// The baseline feeds a CONSENSUS rule (the senderBlacklist fork): every node
// running this binary must agree, byte-for-byte, on which senders are
// blacklisted. Any change to baseline_blacklist.txt — or to ParseBlacklist —
// that alters the resulting set is a consensus-parameter change and MUST be
// a deliberate, reviewed act. This test makes a careless edit fail CI loudly.
func TestBaselineSet_Deterministic(t *testing.T) {
	expected := map[types.Address]struct{}{
		bridgeAttacker: {},
		blacklistProbe: {},
	}

	got := BaselineSet()

	assert.Equal(t, len(expected), len(got),
		"embedded baseline set size changed — this is a CONSENSUS-PARAMETER change; "+
			"update this test deliberately and coordinate a release if intended")
	for addr := range expected {
		assert.Contains(t, got, addr,
			"expected baseline address missing from embedded set: %s", addr)
	}
	for addr := range got {
		assert.Contains(t, expected, addr,
			"unexpected address in embedded baseline set: %s — consensus-parameter change", addr)
	}
}

// TestBaselineSet_ReturnsCopy verifies callers cannot mutate the shared set.
func TestBaselineSet_ReturnsCopy(t *testing.T) {
	a := BaselineSet()
	a[types.StringToAddress("0x1111111111111111111111111111111111111111")] = struct{}{}

	b := BaselineSet()
	assert.NotContains(t, b, types.StringToAddress("0x1111111111111111111111111111111111111111"),
		"BaselineSet() must return an independent copy; callers must not be able to mutate the baseline")
}

func TestIsBaselineBlacklisted(t *testing.T) {
	assert.True(t, IsBaselineBlacklisted(bridgeAttacker),
		"the embedded baseline must flag the bridge attacker")
	assert.True(t, IsBaselineBlacklisted(blacklistProbe),
		"the embedded baseline must flag the deploy-verification probe wallet")
	assert.False(t, IsBaselineBlacklisted(types.StringToAddress("0x2222222222222222222222222222222222222222")),
		"a non-baseline address must not be flagged")
	assert.False(t, IsBaselineBlacklisted(types.ZeroAddress),
		"ZeroAddress must never be blacklisted")
}

func TestParseBlacklist(t *testing.T) {
	t.Run("comments, whitespace, blanks", func(t *testing.T) {
		body := `# leading comment
   0x1111111111111111111111111111111111111111   # inline comment

  0x2222222222222222222222222222222222222222
`
		set := ParseBlacklist(strings.NewReader(body))
		assert.Len(t, set, 2)
		assert.Contains(t, set, types.StringToAddress("0x1111111111111111111111111111111111111111"))
		assert.Contains(t, set, types.StringToAddress("0x2222222222222222222222222222222222222222"))
	})

	t.Run("case-insensitive", func(t *testing.T) {
		set := ParseBlacklist(strings.NewReader("0xABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD\n"))
		assert.Contains(t, set, types.StringToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"))
	})

	t.Run("malformed hex and zero address skipped", func(t *testing.T) {
		body := `0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ
0x0000000000000000000000000000000000000000
0xtoo-short
not-an-address
0x1111111111111111111111111111111111111111
`
		set := ParseBlacklist(strings.NewReader(body))
		assert.Len(t, set, 1)
		assert.Contains(t, set, types.StringToAddress("0x1111111111111111111111111111111111111111"))
		assert.NotContains(t, set, types.ZeroAddress)
	})

	t.Run("empty input yields empty set", func(t *testing.T) {
		assert.Empty(t, ParseBlacklist(strings.NewReader("")))
	})

	t.Run("deterministic across repeated parses", func(t *testing.T) {
		body := "0x1111111111111111111111111111111111111111\n0x2222222222222222222222222222222222222222\n"
		a := ParseBlacklist(strings.NewReader(body))
		b := ParseBlacklist(strings.NewReader(body))
		assert.Equal(t, a, b)
	})
}
