package txpool

import (
	"bufio"
	_ "embed"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xPolygon/polygon-edge/types"
)

// ErrTxBlacklisted is returned by validateTx when the transaction sender
// matches an address in the local blacklist. This is a pool-only check:
// the transaction is refused admission to the local txpool, but the rule is
// not part of consensus — blocks from other validators that contain such
// transactions remain valid. Partial rollout across the validator set is
// therefore safe (no chain split / halt risk).
var ErrTxBlacklisted = errors.New("transaction sender is blacklisted")

// baselineBlacklistRaw is the build-time embedded baseline. Entries here
// apply on every node regardless of operator configuration. The operator
// file (if present) is additive — it can only ADD addresses, never remove
// baseline entries.
//
//go:embed baseline_blacklist.txt
var baselineBlacklistRaw string

const (
	// blacklistFileEnv lets operators override the default operator-file path.
	blacklistFileEnv     = "HYDRA_TX_BLACKLIST_FILE"
	defaultBlacklistFile = "/opt/hydra-blacklist.txt"
	blacklistReloadEvery = 5 * time.Second
	// maxBlacklistEntries bounds memory use against a malformed or
	// hostile-write blacklist file. Practical operator lists are tiny;
	// anything beyond this is a bug or attack.
	maxBlacklistEntries = 100_000
)

type addressSet map[types.Address]struct{}

// blacklist maintains the runtime set of senders whose transactions this
// node refuses to admit to its local pool. The runtime set is the union of
// the build-embedded baseline and the operator-controlled file (if any).
//
// Reads use atomic.Value for lock-free hot-path access. The operator file
// is re-read at most every blacklistReloadEvery; updates are only applied
// if the mtime has changed.
type blacklist struct {
	path string

	// baseline is the immutable build-time set; never mutated after
	// construction. Always present in the runtime union.
	baseline addressSet

	// loaded is the runtime union (baseline ∪ operator-file) loaded under
	// atomic.Value for lock-free reads.
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

	bl := &blacklist{
		path:     path,
		baseline: parseBlacklist(strings.NewReader(baselineBlacklistRaw)),
	}

	// Start with baseline-only; refresh() adds operator-file entries on top.
	bl.loaded.Store(copyAddressSet(bl.baseline))
	bl.refresh()

	return bl
}

// contains reports whether addr is currently blacklisted (baseline ∪ file).
// It triggers a lazy refresh of the operator file at most once per
// blacklistReloadEvery.
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
		// in-place; only a successful read with zero entries clears the
		// operator portion of the union. The baseline is unaffected by
		// stat failures and remains active.
		return
	}

	b.mu.Lock()
	if info.ModTime().Equal(b.mtime) {
		b.mu.Unlock()

		return
	}
	b.mtime = info.ModTime()
	b.mu.Unlock()

	fileSet := loadBlacklistFile(b.path)
	b.loaded.Store(unionAddressSets(b.baseline, fileSet))
}

// loadBlacklistFile parses the operator-controlled blacklist file. Same
// format as the embedded baseline: one address per line, '#' comments,
// blanks ignored, addresses case-insensitive. Bounded by
// maxBlacklistEntries to prevent OOM on a malformed or hostile-write file.
func loadBlacklistFile(path string) addressSet {
	f, err := os.Open(path)
	if err != nil {
		return addressSet{}
	}
	defer f.Close()

	return parseBlacklist(f)
}

// parseBlacklist parses blacklist syntax from any io.Reader, used by both
// the embedded baseline and the operator file path.
func parseBlacklist(r io.Reader) addressSet {
	set := addressSet{}

	scanner := bufio.NewScanner(r)
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
	// Note: scanner.Err() is intentionally ignored. A malformed file (e.g.
	// line >64 KiB exceeding bufio default) yields a partial set; we prefer
	// "best-effort filter" to "no filter" given the safety invariant.

	return set
}

func copyAddressSet(s addressSet) addressSet {
	out := make(addressSet, len(s))
	for k := range s {
		out[k] = struct{}{}
	}

	return out
}

func unionAddressSets(a, b addressSet) addressSet {
	out := make(addressSet, len(a)+len(b))
	for k := range a {
		out[k] = struct{}{}
	}
	for k := range b {
		out[k] = struct{}{}
	}

	return out
}
