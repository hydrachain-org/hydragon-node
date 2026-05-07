package txpool

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xPolygon/polygon-edge/types"
)

// ErrTxBlacklisted is returned by validateTx when the transaction sender
// matches an address in the local blacklist file. This is a pool-only check:
// the transaction is refused admission to the local txpool, but the rule is
// not part of consensus — blocks from other validators that contain such
// transactions remain valid. Partial rollout across the validator set is
// therefore safe (no chain split / halt risk).
var ErrTxBlacklisted = errors.New("transaction sender is blacklisted")

// blacklistFileEnv lets operators override the default blacklist path.
const (
	blacklistFileEnv     = "HYDRA_TX_BLACKLIST_FILE"
	defaultBlacklistFile = "/opt/hydra-blacklist.txt"
	blacklistReloadEvery = 5 * time.Second
	// maxBlacklistEntries bounds memory use against a malformed or
	// hostile-write blacklist file. Practical operator lists are tiny
	// (single-digit entries); anything beyond this is a bug or attack.
	maxBlacklistEntries = 100_000
)

type addressSet map[types.Address]struct{}

// blacklist maintains the set of senders whose transactions this node refuses
// to admit to its local pool. Reads use atomic.Value for lock-free hot-path
// access. The file is re-read at most every blacklistReloadEvery; updates are
// only applied if the mtime has changed.
type blacklist struct {
	path string

	loaded atomic.Value // addressSet

	mu       sync.Mutex
	mtime    time.Time
	lastPoll time.Time
}

func newBlacklist() *blacklist {
	path := os.Getenv(blacklistFileEnv)
	if path == "" {
		path = defaultBlacklistFile
	}

	bl := &blacklist{path: path}
	bl.loaded.Store(addressSet{})
	bl.refresh()

	return bl
}

// contains reports whether addr is currently blacklisted. It triggers a lazy
// refresh of the underlying file at most once per blacklistReloadEvery.
func (b *blacklist) contains(addr types.Address) bool {
	b.maybeRefresh()

	set, _ := b.loaded.Load().(addressSet)
	_, found := set[addr]

	return found
}

func (b *blacklist) maybeRefresh() {
	b.mu.Lock()
	now := time.Now()
	if now.Sub(b.lastPoll) < blacklistReloadEvery {
		b.mu.Unlock()

		return
	}

	b.lastPoll = now
	b.mu.Unlock()

	b.refresh()
}

func (b *blacklist) refresh() {
	info, err := os.Stat(b.path)
	if err != nil {
		// Keep whatever set was previously loaded. A transient stat failure
		// (EMFILE, brief unmount, ENOENT during atomic-rename swap) must not
		// silently clear the filter — that would open a window in which a
		// blacklisted sender's tx could be admitted. Operators must clear
		// entries by truncating or removing addresses from the file
		// in-place; only a successful read with zero entries clears the set.
		return
	}

	b.mu.Lock()
	if info.ModTime().Equal(b.mtime) {
		b.mu.Unlock()

		return
	}
	b.mtime = info.ModTime()
	b.mu.Unlock()

	set := loadBlacklistFile(b.path)
	b.loaded.Store(set)
}

// loadBlacklistFile parses the blacklist. One address per line, lines starting
// with '#' are comments, blanks ignored, addresses case-insensitive.
// Malformed lines are skipped (not fatal — operator-controlled file).
// Entries are bounded by maxBlacklistEntries to prevent OOM on a malformed
// or hostile-write file.
func loadBlacklistFile(path string) addressSet {
	set := addressSet{}

	f, err := os.Open(path)
	if err != nil {
		return set
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(set) >= maxBlacklistEntries {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip inline comments
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		// IsValidAddress validates 0x-prefix, length, and hex encoding.
		// Malformed lines (e.g. "0xZZ...", short hex, missing prefix)
		// produce a non-nil error and are skipped, preventing silent
		// pollution of the set with types.ZeroAddress.
		if err := types.IsValidAddress(line); err != nil {
			continue
		}

		addr := types.StringToAddress(strings.ToLower(line))
		if addr == types.ZeroAddress {
			// Defensive: signer.Sender() never recovers ZeroAddress on a
			// valid signature, so blacklisting it can never trigger on
			// legitimate traffic — but keeping this entry would be a
			// foot-gun for future signer changes.
			continue
		}

		set[addr] = struct{}{}
	}
	// Note: scanner.Err() is intentionally ignored here. A malformed file
	// (e.g. line >64 KiB exceeding bufio default) yields a partial set; we
	// prefer "best-effort filter" to "no filter" given the safety claim.

	return set
}
