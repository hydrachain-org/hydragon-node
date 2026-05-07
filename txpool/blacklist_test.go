package txpool

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestBlacklist_NoFile_Empty(t *testing.T) {
	t.Setenv(blacklistFileEnv, "/nonexistent/blacklist-does-not-exist.txt")

	bl := newBlacklist()
	addr := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")

	assert.False(t, bl.contains(addr))
}

func TestBlacklist_EmptyFile_Empty(t *testing.T) {
	path := writeBlacklist(t, "")
	t.Setenv(blacklistFileEnv, path)

	bl := newBlacklist()
	addr := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")

	assert.False(t, bl.contains(addr))
}

func TestBlacklist_ContainsBlacklistedAddress(t *testing.T) {
	path := writeBlacklist(t, "0xd06e82e2acd26848f86d0f559f7037cd8896071b\n")
	t.Setenv(blacklistFileEnv, path)

	bl := newBlacklist()

	hacker := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")
	other := types.StringToAddress("0x1111111111111111111111111111111111111111")

	assert.True(t, bl.contains(hacker))
	assert.False(t, bl.contains(other))
}

func TestBlacklist_CaseInsensitive(t *testing.T) {
	// Mixed-case write, all-lower lookup must match
	path := writeBlacklist(t, "0xD06E82E2ACD26848F86D0F559F7037CD8896071B\n")
	t.Setenv(blacklistFileEnv, path)

	bl := newBlacklist()

	addr := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")
	assert.True(t, bl.contains(addr))
}

func TestBlacklist_TolerantToCommentsAndWhitespace(t *testing.T) {
	body := `# leading comment
   0xd06e82e2acd26848f86d0f559f7037cd8896071b   # inline comment

# blank line above

  0x1111111111111111111111111111111111111111
not-an-address
0xtoo-short
`
	path := writeBlacklist(t, body)
	t.Setenv(blacklistFileEnv, path)

	bl := newBlacklist()

	hacker := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")
	second := types.StringToAddress("0x1111111111111111111111111111111111111111")
	other := types.StringToAddress("0x2222222222222222222222222222222222222222")

	assert.True(t, bl.contains(hacker))
	assert.True(t, bl.contains(second))
	assert.False(t, bl.contains(other))
}

// TestBlacklist_ReloadsOnFileChange exercises the lazy mtime-based reload.
// We bypass the 5-second poll throttle by resetting lastPoll between checks.
func TestBlacklist_ReloadsOnFileChange(t *testing.T) {
	path := writeBlacklist(t, "")
	t.Setenv(blacklistFileEnv, path)

	bl := newBlacklist()
	addr := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")

	assert.False(t, bl.contains(addr), "should not be blacklisted before file is populated")

	// Sleep enough for mtime resolution then write the new content.
	time.Sleep(20 * time.Millisecond)
	assert.NoError(t, os.WriteFile(path, []byte("0xd06e82e2acd26848f86d0f559f7037cd8896071b\n"), 0600))

	// Force a poll on next call
	bl.mu.Lock()
	bl.lastPoll = time.Time{}
	bl.mu.Unlock()

	assert.True(t, bl.contains(addr), "should pick up newly-added address")

	// Now remove the address by truncating the file
	time.Sleep(20 * time.Millisecond)
	assert.NoError(t, os.WriteFile(path, []byte(""), 0600))

	bl.mu.Lock()
	bl.lastPoll = time.Time{}
	bl.mu.Unlock()

	assert.False(t, bl.contains(addr), "should drop entry after file is emptied")
}

func TestBlacklist_StatErrorPreservesSet(t *testing.T) {
	// Load a populated file, then make it disappear, then prove the
	// in-memory set is preserved (NOT cleared) on the next refresh.
	dir := t.TempDir()
	path := filepath.Join(dir, "blacklist.txt")
	assert.NoError(t, os.WriteFile(path, []byte("0xd06e82e2acd26848f86d0f559f7037cd8896071b\n"), 0600))

	t.Setenv(blacklistFileEnv, path)
	bl := newBlacklist()

	hacker := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")
	assert.True(t, bl.contains(hacker), "should be loaded after constructor")

	// Remove the file — simulates transient I/O failure.
	assert.NoError(t, os.Remove(path))

	// Force the next refresh by clearing throttle.
	bl.mu.Lock()
	bl.lastPoll = time.Time{}
	bl.mu.Unlock()

	// MUST still be blacklisted — stat error must not clear the set.
	assert.True(t, bl.contains(hacker),
		"transient stat error must NOT clear the in-memory blacklist; this guards against I/O blips opening admission windows")
}

func TestBlacklist_RejectsMalformedHex(t *testing.T) {
	// 42-char strings that look right but contain non-hex must NOT
	// pollute the set with types.ZeroAddress.
	body := `0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ
0x0000000000000000000000000000000000000000
0xd06e82e2acd26848f86d0f559f7037cd8896071b
`
	path := writeBlacklist(t, body)
	t.Setenv(blacklistFileEnv, path)

	bl := newBlacklist()

	// Real entry survives.
	assert.True(t, bl.contains(types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")))

	// ZeroAddress must NOT be in the set, even though the file contained it.
	assert.False(t, bl.contains(types.ZeroAddress),
		"ZeroAddress must never be blacklisted; signer recovery never returns it on valid signatures, so the only way it ends up in the set is malformed-hex pollution")
}

func TestBlacklist_PollThrottling(t *testing.T) {
	path := writeBlacklist(t, "")
	t.Setenv(blacklistFileEnv, path)

	bl := newBlacklist()
	addr := types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b")

	assert.False(t, bl.contains(addr))

	// Write new content but DO NOT clear lastPoll. Throttling should mean
	// the change is not picked up on the very next call.
	time.Sleep(20 * time.Millisecond)
	assert.NoError(t, os.WriteFile(path, []byte("0xd06e82e2acd26848f86d0f559f7037cd8896071b\n"), 0600))

	// First call after lastPoll set during constructor — within throttle window.
	assert.False(t, bl.contains(addr), "throttle should suppress immediate reload")
}
