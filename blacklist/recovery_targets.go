package blacklist

import (
	"github.com/0xPolygon/polygon-edge/types"
)

// senderBlacklist FORK RECOVERY TARGETS (consensus parameters).
//
// These are state-transition constants applied at exactly the senderBlacklist
// fork activation block. They must be byte-identical across every patched node
// — any drift splits the chain. Edits to this file are CONSENSUS-PARAMETER
// changes and require a coordinated binary release.
//
// At the activation block, ApplyOneShotRecovery (state/executor.go) fires once:
//   - For every native source: balance(src) -> 0, balance(RecoveryDestination) += old_balance.
//   - For every (RecoveryERC20Targets[i], native source) pair: the OpenZeppelin
//     _balances mapping slot is mutated directly — source's slot zeroed,
//     destination's slot incremented by the same amount. _totalSupply (slot 2)
//     is NOT touched: it's a transfer within the contract; supply conserved.
//
// Idempotency: re-execution of the activation block (reorg/replay) re-reads
// already-zeroed sources and is a no-op.

// RecoveryDestination is the address that receives all recovered native HYDRA
// and ERC20 balances at activation. Hydragon DAO Safe multisig (2/3, per
// CLAUDE.md §5 bridge governance).
var RecoveryDestination = types.StringToAddress("0x1B4A1b89cEfBa22a8B7D6469Ef52b9fd20f8FC04")

// RecoverySources are the holders whose entire native HYDRA balance and all
// ERC20 balances (across RecoveryERC20Targets) are transferred to
// RecoveryDestination at activation.
//
// !!! POLICY — READ BEFORE EDITING !!!
// Any balance held by an address in this list at the activation block — native
// HYDRA AND every RecoveryERC20Targets ERC20 — is unconditionally and
// irreversibly transferred to RecoveryDestination (DAO Safe). This applies on
// EVERY network the patched binary runs (mainnet, testnet, devnet). There is
// no chain-ID gate: the consensus rule must be byte-identical across all
// nodes, so any per-chain branching here would split state-root computations.
//
// Order is significant for the conservation log only — math is commutative.
//
// Entries:
//  1. Bridge attacker (May-2026 incident wallet, drained ~770k HYDRA native
//     + phantom-minted ~9e29 wei in 8 wrapped ERC20s on Hydragon mainnet).
//  2. Operator verification probe — operator-controlled MetaMask wallet
//     (key held by Hydra ops; address recorded in /opt/.secrets as
//     HYDRA_BLACKLIST_PROBE_ADDR). On testnet/devnet we deliberately
//     pre-fund this wallet with native + ERC20 fixtures so the state delta
//     exercises every branch against a wallet we control. On MAINNET the
//     probe wallet ALSO drains to the DAO Safe at activation — by policy,
//     ANY balance present is treated as "DAO assets via fork." Operators
//     MUST NOT use this address for anything other than verification dust.
//     The address remains permanently blacklisted by the txpool filter
//     (PR #137) so the wallet cannot accidentally transact post-activation.
var RecoverySources = []types.Address{
	types.StringToAddress("0xd06e82e2acd26848f86d0f559f7037cd8896071b"),
	types.StringToAddress("0xcf4d5bf9ea8cc3cf083c9ee88305c5cce87947eb"),
}

// RecoveryERC20Targets are the V1-wrapped ERC20 contracts on Hydragon where
// the attacker holds phantom-minted balances. For each, the _balances mapping
// is at storage slot 0 (OZ-standard, verified on-chain against balanceOf() for
// all 8 contracts at design time).
var RecoveryERC20Targets = []types.Address{
	types.StringToAddress("0x71f16bCb805566F322a0904885D6147a76C249C5"), // hyUSD
	types.StringToAddress("0xb8043294eFf43bcD01BD33968c7ae9dbc6A4BF8B"), // WBTC
	types.StringToAddress("0x7b29E92ba491CD0AAcb64A363af2AfC2931Fd9F4"), // USDT
	types.StringToAddress("0xbBf6f2d2D462185dF545c744974b7Eb6ddadFcfd"), // USDC
	types.StringToAddress("0xD185683dfb59e26E269e00CE1DE75942c3d5fCD2"), // ETH (wrapped)
	types.StringToAddress("0xDf382759Ea2393a26d2F71924A4f69962ff0C1c5"), // DAI
	types.StringToAddress("0x8C3814babbc5DAdCcee2b1b448B7a037EC4fc6f2"), // LOC
	types.StringToAddress("0x18FFd46709F9EBc7c686FBdD8E2A7531Ce34869F"), // CHANGE
}

// ERC20BalancesSlot is the storage slot of the _balances mapping in
// OpenZeppelin-standard ERC20 contracts. The per-holder slot is computed as
// keccak256(pad(holder, 32) || pad(ERC20BalancesSlot, 32)).
const ERC20BalancesSlot uint64 = 0
