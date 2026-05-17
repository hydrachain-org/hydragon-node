// Package blacklist owns the build-time embedded sender baseline blacklist.
//
// It is the single source of truth for the baseline set, shared by two
// independent consumers:
//
//   - the txpool admission filter (package txpool) — pool-only, may union the
//     baseline with a host-local operator file;
//   - the consensus rule (package state, gated by the senderBlacklist fork) —
//     which consults ONLY the baseline via IsBaselineBlacklisted.
//
// The baseline is embedded at build time, so it is byte-identical and the
// parse result is deterministic across every node running the same binary —
// a hard prerequisite for using it in a consensus rule. The operator-
// controlled file is deliberately NOT part of this package: it is host-
// specific and hot-editable, and must never reach the consensus path.
//
// Changing baseline_blacklist.txt is therefore a consensus-parameter change
// and must be treated with the same gravity as a fork-block change. The
// determinism test in baseline_test.go pins the parsed set so a careless
// edit fails CI loudly.
package blacklist

import (
	"bufio"
	_ "embed"
	"io"
	"strings"

	"github.com/0xPolygon/polygon-edge/types"
)

// maxBlacklistEntries bounds the parsed set size against a malformed file.
// Practical lists are tiny; anything beyond this is a bug.
const maxBlacklistEntries = 100_000

//go:embed baseline_blacklist.txt
var baselineRaw string

// baselineSet is the parsed embedded baseline. Initialized in init() — which
// the Go spec guarantees runs after the //go:embed variable is populated —
// and never mutated afterwards.
var baselineSet map[types.Address]struct{}

func init() {
	baselineSet = ParseBlacklist(strings.NewReader(baselineRaw))
}

// ParseBlacklist parses blacklist syntax from any io.Reader: one address per
// line, '#' starts a line- or inline-comment, blank lines are ignored,
// addresses are case-insensitive. Malformed lines (bad hex, wrong length,
// missing prefix) are skipped rather than poisoning the set. Bounded by
// maxBlacklistEntries. For identical input the result is deterministic —
// this property is what makes the baseline safe to use as a consensus rule.
func ParseBlacklist(r io.Reader) map[types.Address]struct{} {
	set := make(map[types.Address]struct{})

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if len(set) >= maxBlacklistEntries {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip inline comments.
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		// IsValidAddress validates 0x-prefix, length, and hex encoding.
		// Malformed lines produce a non-nil error and are skipped,
		// preventing silent pollution of the set with types.ZeroAddress.
		if err := types.IsValidAddress(line); err != nil {
			continue
		}

		addr := types.StringToAddress(strings.ToLower(line))
		if addr == types.ZeroAddress {
			// Defensive: signer.Sender() never recovers ZeroAddress on a
			// valid signature, so blacklisting it can never trigger on
			// legitimate traffic — but keeping the entry would be a
			// foot-gun for future signer changes.
			continue
		}

		set[addr] = struct{}{}
	}

	return set
}

// BaselineSet returns a fresh copy of the parsed embedded baseline set.
// Callers (e.g. the txpool) get their own copy and may union it with other
// sources; the package-internal baseline is never mutated.
func BaselineSet() map[types.Address]struct{} {
	out := make(map[types.Address]struct{}, len(baselineSet))
	for k := range baselineSet {
		out[k] = struct{}{}
	}

	return out
}

// IsBaselineBlacklisted reports whether addr is in the build-embedded
// baseline blacklist.
//
// This is the ONLY blacklist source the consensus rule (package state) may
// consult. It is deterministic across all nodes running the same binary.
// The operator file must never reach this path.
func IsBaselineBlacklisted(addr types.Address) bool {
	_, found := baselineSet[addr]

	return found
}
