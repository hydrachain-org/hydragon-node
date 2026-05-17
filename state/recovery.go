package state

import (
	"math/big"

	"github.com/0xPolygon/polygon-edge/blacklist"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
)

// ApplyOneShotRecovery runs at exactly the senderBlacklist fork activation
// block. It transfers — within a single atomic state mutation — every native
// HYDRA balance and every ERC20 balance held by blacklist.RecoverySources to
// blacklist.RecoveryDestination (the Hydragon DAO Safe).
//
// CALL SITES (write paths only — DO NOT call from read-only state queries):
//   - consensus/polybft/block_builder.go::Reset (proposer side, right after BeginTxn)
//   - consensus/polybft/blockchain_wrapper.go::ProcessBlock (verifier side, right after BeginTxn)
//   - state/executor.go::ProcessBlock (legacy / generic block-import path)
//
// All state changes happen BEFORE any user transactions in the activation
// block apply. Effects:
//   - sources native balance -> 0; destination native += sum of sources
//   - per ERC20 token in blacklist.RecoveryERC20Targets:
//     sources' _balances slot -> 0; destination's _balances slot += sum
//     _totalSupply (slot 2) is NOT mutated — transfer within contract
//
// Invariants enforced by construction:
//   - Native: sum(balances) over universe unchanged (transfer, not burn).
//   - ERC20: per-contract totalSupply unchanged; sum of mutated _balances
//     entries before == after (transfer).
//
// Idempotency: on re-execution of the activation block (reorg/replay), the
// source balances are already 0, so every per-source/per-token branch is a
// no-op (zero-sum transfer). Fork-active boolean check is for safety:
// SenderBlacklistBlock is non-zero only when the fork is configured.
func ApplyOneShotRecovery(t *Transition, blockNumber uint64) {
	// Only fire at exactly the activation block, and only if the fork is
	// configured. The bool guards against accidental wiring on a chain that
	// has not opted into the fork via its genesis.
	if !t.config.SenderBlacklist {
		return
	}

	if t.config.SenderBlacklistBlock == 0 || blockNumber != t.config.SenderBlacklistBlock {
		return
	}

	dst := blacklist.RecoveryDestination

	// --- native HYDRA: transfer source balances to DAO ---
	for _, src := range blacklist.RecoverySources {
		bal := t.state.GetBalance(src)
		if bal.Sign() == 0 {
			continue
		}

		// SubBalance returns error only on insufficient balance; we just read
		// the exact balance so this cannot fail. Defensive check just in case.
		if err := t.state.SubBalance(src, bal); err != nil {
			t.logger.Error("senderBlacklist recovery: failed to drain native source",
				"src", src, "balance", bal.String(), "err", err)

			continue
		}

		t.state.AddBalance(dst, bal)
		t.logger.Warn("senderBlacklist recovery: native drained to DAO Safe",
			"src", src, "dst", dst, "amount_wei", bal.String())
	}

	// --- ERC20: transfer source _balances entries to DAO within each contract ---
	dstSlot := erc20BalanceSlot(dst)

	for _, token := range blacklist.RecoveryERC20Targets {
		for _, src := range blacklist.RecoverySources {
			srcSlot := erc20BalanceSlot(src)
			srcBalHash := t.state.GetState(token, srcSlot)

			srcBal := new(big.Int).SetBytes(srcBalHash.Bytes())
			if srcBal.Sign() == 0 {
				continue
			}

			dstBalHash := t.state.GetState(token, dstSlot)
			dstBal := new(big.Int).SetBytes(dstBalHash.Bytes())
			newDst := new(big.Int).Add(dstBal, srcBal)

			t.state.SetState(token, srcSlot, types.Hash{})
			t.state.SetState(token, dstSlot, bigIntToHash(newDst))

			t.logger.Warn("senderBlacklist recovery: ERC20 drained to DAO Safe",
				"token", token, "src", src, "dst", dst, "amount_wei", srcBal.String())
		}
	}
}

// erc20BalanceSlot returns the storage key for `_balances[holder]` in an
// OpenZeppelin-standard ERC20 contract, i.e.
// keccak256(abi.encode(holder, blacklist.ERC20BalancesSlot)).
//
// Layout: 32 bytes for the address (left-padded with 12 zero bytes), followed
// by 32 bytes for the mapping slot (uint256, big-endian).
func erc20BalanceSlot(holder types.Address) types.Hash {
	var buf [64]byte

	copy(buf[12:32], holder[:])
	// buf[32:64] is already zero, which is the correct big-endian encoding of
	// blacklist.ERC20BalancesSlot (0). If that constant ever changes, set
	// buf[63] = byte(blacklist.ERC20BalancesSlot) etc.
	if blacklist.ERC20BalancesSlot != 0 {
		// Future-proof: encode the slot as a big-endian uint64 in the last 8 bytes.
		// Compatible for slot values 0..2^64-1.
		s := blacklist.ERC20BalancesSlot
		for i := 7; i >= 0; i-- {
			buf[56+i] = byte(s & 0xff)
			s >>= 8
		}
	}

	return types.BytesToHash(crypto.Keccak256(buf[:]))
}

// bigIntToHash encodes a non-negative big.Int as a left-padded 32-byte Hash,
// suitable for writing to EVM storage.
func bigIntToHash(v *big.Int) types.Hash {
	var h types.Hash

	b := v.Bytes()
	if len(b) > 32 {
		// Defensive: an ERC20 balance fitting in uint256 cannot exceed 32 bytes.
		// If we ever hit this path the consensus rule has already lost — log
		// loudly and write the low-order 32 bytes, which preserves nothing
		// useful but at least is deterministic.
		copy(h[:], b[len(b)-32:])

		return h
	}

	copy(h[32-len(b):], b)

	return h
}
