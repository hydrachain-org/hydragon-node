package prune

import (
	"github.com/spf13/cobra"
)

func GetCommand() *cobra.Command {
	pruneCmd := &cobra.Command{
		Use:   "prune-trie",
		Short: "Prunes historical state trie data by copying only reachable nodes to a new database",
		Long: `Prunes the state trie LevelDB by copying only nodes reachable from the latest
(or specified) block's state root to a new directory. The source trie is opened
read-only and never modified. After validation, the operator can swap directories.

Usage:
  hydra prune-trie --data-dir ./node-secrets --target-path ./trie_new
  hydra prune-trie --data-dir ./node-secrets --target-path ./trie_new --block 50000`,
	}

	pruneCmd.AddCommand(pruneTrieCmd())

	return pruneCmd
}
