package txpool

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/blacklist"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
)

func writeBlacklist(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "blacklist.txt")
	assert.NoError(t, os.WriteFile(path, []byte(body), 0600))

	return path
}

// hackerAddr is the address embedded in the build-time baseline
// (txpool/baseline_blacklist.txt). Tests assert it's blocked unconditionally
// — even when no operator file exists.
var hackerAddr = types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")

func TestBlacklist_BaselineBlocksHackerWithNoOperatorFile(t *testing.T) {
	t.Setenv(blacklistFileEnv, "/nonexistent/blacklist-does-not-exist.txt")

	bl := newPoolBlacklist()

	assert.True(t, bl.contains(hackerAddr),
		"baseline must block the hacker even with no operator file present")
}

func TestBlacklist_BaselineBlocksHackerWithEmptyOperatorFile(t *testing.T) {
	path := writeBlacklist(t, "")
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()

	assert.True(t, bl.contains(hackerAddr),
		"baseline must block the hacker even when the operator file is empty")
}

func TestBlacklist_OperatorFileIsAdditive(t *testing.T) {
	// Operator adds a separate test wallet; baseline still blocks hacker.
	other := types.StringToAddress("0x1111111111111111111111111111111111111111")
	path := writeBlacklist(t, "0x1111111111111111111111111111111111111111\n")
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()

	assert.True(t, bl.contains(hackerAddr), "baseline still active")
	assert.True(t, bl.contains(other), "operator-file entry blocked too")
}

func TestBlacklist_OperatorRemovalDoesNotUnblockBaseline(t *testing.T) {
	// Operator file initially has the hacker; then operator clears the file.
	// The hacker MUST stay blocked because the baseline owns the entry.
	path := writeBlacklist(t, "0xd06e82e2acd26848f86d0f559f7037cd8896071b\n")
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()
	assert.True(t, bl.contains(hackerAddr))

	// Truncate operator file and force a refresh
	time.Sleep(20 * time.Millisecond)
	assert.NoError(t, os.WriteFile(path, []byte(""), 0600))

	bl.mu.Lock()
	bl.lastPoll = time.Time{}
	bl.mu.Unlock()

	assert.True(t, bl.contains(hackerAddr),
		"removing hacker from operator file must NOT unblock the baseline entry")
}

func TestBlacklist_NoOperatorFile_BaselineStillActive(t *testing.T) {
	t.Setenv(blacklistFileEnv, "/nonexistent/blacklist-does-not-exist.txt")

	bl := newPoolBlacklist()

	other := types.StringToAddress("0x2222222222222222222222222222222222222222")
	assert.True(t, bl.contains(hackerAddr), "baseline blocks hacker")
	assert.False(t, bl.contains(other), "non-baseline non-file address is not blocked")
}

func TestBlacklist_ContainsBlacklistedAddress(t *testing.T) {
	other := types.StringToAddress("0x1234567890abcdef1234567890abcdef12345678")
	path := writeBlacklist(t, "0x1234567890abcdef1234567890abcdef12345678\n")
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()

	assert.True(t, bl.contains(other))
	other2 := types.StringToAddress("0x9999999999999999999999999999999999999999")
	assert.False(t, bl.contains(other2))
}

func TestBlacklist_CaseInsensitive(t *testing.T) {
	// Mixed-case write, all-lower lookup must match
	path := writeBlacklist(t, "0xABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD\n")
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()

	addr := types.StringToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	assert.True(t, bl.contains(addr))
}

func TestBlacklist_TolerantToCommentsAndWhitespace(t *testing.T) {
	body := `# leading comment
   0x1111111111111111111111111111111111111111   # inline comment

# blank line above

  0x2222222222222222222222222222222222222222
not-an-address
0xtoo-short
`
	path := writeBlacklist(t, body)
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()

	first := types.StringToAddress("0x1111111111111111111111111111111111111111")
	second := types.StringToAddress("0x2222222222222222222222222222222222222222")
	other := types.StringToAddress("0x3333333333333333333333333333333333333333")

	assert.True(t, bl.contains(first))
	assert.True(t, bl.contains(second))
	assert.False(t, bl.contains(other))
}

// TestBlacklist_ReloadsOnFileChange exercises the lazy mtime-based reload.
// We bypass the 5-second poll throttle by resetting lastPoll between checks.
func TestBlacklist_ReloadsOnFileChange(t *testing.T) {
	addr := types.StringToAddress("0x1111111111111111111111111111111111111111")
	path := writeBlacklist(t, "")
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()

	assert.False(t, bl.contains(addr), "should not be blacklisted before file is populated")

	time.Sleep(20 * time.Millisecond)
	assert.NoError(t, os.WriteFile(path, []byte("0x1111111111111111111111111111111111111111\n"), 0600))

	bl.mu.Lock()
	bl.lastPoll = time.Time{}
	bl.mu.Unlock()

	assert.True(t, bl.contains(addr), "should pick up newly-added address from operator file")

	// Now remove the address by truncating the file
	time.Sleep(20 * time.Millisecond)
	assert.NoError(t, os.WriteFile(path, []byte(""), 0600))

	bl.mu.Lock()
	bl.lastPoll = time.Time{}
	bl.mu.Unlock()

	assert.False(t, bl.contains(addr), "should drop entry after file is emptied")
	assert.True(t, bl.contains(hackerAddr), "baseline unaffected")
}

func TestBlacklist_StatErrorPreservesSet(t *testing.T) {
	// Load a populated file, then make it disappear, then prove the
	// in-memory set (file portion) is preserved on the next refresh.
	addr := types.StringToAddress("0x1111111111111111111111111111111111111111")
	dir := t.TempDir()
	path := filepath.Join(dir, "blacklist.txt")
	assert.NoError(t, os.WriteFile(path, []byte("0x1111111111111111111111111111111111111111\n"), 0600))

	t.Setenv(blacklistFileEnv, path)
	bl := newPoolBlacklist()

	assert.True(t, bl.contains(addr), "operator-file entry loaded after constructor")
	assert.True(t, bl.contains(hackerAddr), "baseline always loaded")

	// Remove the file — simulates transient I/O failure.
	assert.NoError(t, os.Remove(path))

	// Force the next refresh by clearing throttle.
	bl.mu.Lock()
	bl.lastPoll = time.Time{}
	bl.mu.Unlock()

	// MUST still be blacklisted — stat error must not clear the union.
	assert.True(t, bl.contains(addr),
		"transient stat error must NOT clear the in-memory blacklist union")
	assert.True(t, bl.contains(hackerAddr),
		"baseline always remains active regardless of file state")
}

func TestBlacklist_RejectsMalformedHex(t *testing.T) {
	// 42-char strings that look right but contain non-hex must NOT
	// pollute the set with types.ZeroAddress.
	body := `0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ
0x0000000000000000000000000000000000000000
0x1111111111111111111111111111111111111111
`
	path := writeBlacklist(t, body)
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()

	first := types.StringToAddress("0x1111111111111111111111111111111111111111")
	assert.True(t, bl.contains(first))
	assert.False(t, bl.contains(types.ZeroAddress),
		"ZeroAddress must never be blacklisted; signer recovery never returns it on valid signatures")
}

func TestBlacklist_PollThrottling(t *testing.T) {
	addr := types.StringToAddress("0x1111111111111111111111111111111111111111")
	path := writeBlacklist(t, "")
	t.Setenv(blacklistFileEnv, path)

	bl := newPoolBlacklist()

	assert.False(t, bl.contains(addr))

	// Write new content but DO NOT clear lastPoll. Throttling should mean
	// the change is not picked up on the very next call.
	time.Sleep(20 * time.Millisecond)
	assert.NoError(t, os.WriteFile(path, []byte("0x1111111111111111111111111111111111111111\n"), 0600))

	assert.False(t, bl.contains(addr), "throttle should suppress immediate reload")
}

func TestBlacklist_BaselineFileIsParseable(t *testing.T) {
	// Sanity: the build-embedded baseline (now owned by the shared blacklist
	// package) must parse to a non-empty set including the bridge attacker.
	// The authoritative determinism test lives in blacklist/baseline_test.go.
	set := blacklist.BaselineSet()
	assert.NotEmpty(t, set, "embedded baseline must contain at least one address")
	assert.Contains(t, set, hackerAddr, "embedded baseline must include the bridge attacker")
}
