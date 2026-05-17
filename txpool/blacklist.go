package txpool

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xPolygon/polygon-edge/blacklist"
	"github.com/0xPolygon/polygon-edge/types"
)

// ErrTxBlacklisted is returned by validateTx when the transaction sender
// matches an address in the local blacklist. This is a POOL-ONLY check:
// the transaction is refused admission to the local txpool, but the rule is
// not, by itself, part of consensus. The consensus rule lives separately in
// the state package (gated by the senderBlacklist fork) and consults ONLY
// the build-embedded baseline — never the operator file handled here.
var ErrTxBlacklisted = errors.New("transaction sender is blacklisted")

const (
	// blacklistFileEnv lets operators override the default operator-file path.
	blacklistFileEnv     = "HYDRA_TX_BLACKLIST_FILE"
	defaultBlacklistFile = "/opt/hydra-blacklist.txt"
	blacklistReloadEvery = 5 * time.Second
)

// addressSet is a type alias so that values returned by the shared blacklist
// package (map[types.Address]struct{}) assign directly into poolBlacklist.
type addressSet = map[types.Address]struct{}

// poolBlacklist maintains the runtime set of senders whose transactions this
// node refuses to admit to its local pool. The runtime set is the union of
// the build-embedded baseline (owned by the shared blacklist package) and the
// operator-controlled file (if any).
//
// Reads use atomic.Value for lock-free hot-path access. The operator file is
// re-read at most every blacklistReloadEvery; updates are only applied if the
// mtime has changed.
//
// The operator-file portion is host-specific and hot-editable. It is
// deliberately confined to this pool-only path and must never feed the
// consensus rule in the state package.
type poolBlacklist struct {
	path string

	// baseline is the immutable build-time set, sourced from the shared
	// blacklist package; never mutated after construction. Always present in
	// the runtime union.
	baseline addressSet

	// loaded is the runtime union (baseline ∪ operator-file) loaded under
	// atomic.Value for lock-free reads.
	loaded atomic.Value // addressSet

	mu       sync.Mutex
	mtime    time.Time
	lastPoll time.Time
}

func newPoolBlacklist() *poolBlacklist {
	path := os.Getenv(blacklistFileEnv)
	if path == "" {
		path = defaultBlacklistFile
	}

	bl := &poolBlacklist{
		path:     path,
		baseline: blacklist.BaselineSet(),
	}

	// Start with baseline-only; refresh() adds operator-file entries on top.
	bl.loaded.Store(copyAddressSet(bl.baseline))
	bl.refresh()

	return bl
}

// contains reports whether addr is currently blacklisted (baseline ∪ file).
// It triggers a lazy refresh of the operator file at most once per
// blacklistReloadEvery.
func (b *poolBlacklist) contains(addr types.Address) bool {
	b.maybeRefresh()

	set, _ := b.loaded.Load().(addressSet)
	_, found := set[addr]

	return found
}

func (b *poolBlacklist) maybeRefresh() {
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

func (b *poolBlacklist) refresh() {
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
// blanks ignored, addresses case-insensitive.
func loadBlacklistFile(path string) addressSet {
	f, err := os.Open(path)
	if err != nil {
		return addressSet{}
	}
	defer f.Close()

	return blacklist.ParseBlacklist(f)
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
