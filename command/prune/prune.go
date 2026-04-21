package prune

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	leveldb2 "github.com/0xPolygon/polygon-edge/blockchain/storage/leveldb"
	"github.com/0xPolygon/polygon-edge/command"
	itrie "github.com/0xPolygon/polygon-edge/state/immutable-trie"
	"github.com/0xPolygon/polygon-edge/types"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/spf13/cobra"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

type pruneParams struct {
	DataDir    string
	TargetPath string
	BlockNum   uint64
}

var params pruneParams

func pruneTrieCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the trie pruning operation",
	}

	cmd.Flags().StringVar(
		&params.DataDir, "data-dir", "",
		"path to node data directory (contains trie/ and blockchain/ subdirs)",
	)
	cmd.Flags().StringVar(&params.TargetPath, "target-path", "", "path for the new pruned trie database")
	cmd.Flags().Uint64Var(&params.BlockNum, "block", 0, "block number to prune at (default: latest)")

	outputter := command.InitializeOutputter(cmd)

	cmd.Run = func(cmd *cobra.Command, args []string) {
		defer outputter.WriteOutput()

		if err := validateParams(); err != nil {
			outputter.SetError(err)

			return
		}

		result, err := runPrune()
		if err != nil {
			outputter.SetError(err)

			return
		}

		outputter.WriteCommandResult(result)
	}

	return cmd
}

func validateParams() error {
	if params.DataDir == "" {
		return fmt.Errorf("--data-dir is required")
	}

	if params.TargetPath == "" {
		return fmt.Errorf("--target-path is required")
	}

	triePath := filepath.Join(params.DataDir, "trie")
	if _, err := os.Stat(triePath); os.IsNotExist(err) {
		return fmt.Errorf("trie directory not found at %s", triePath)
	}

	blockchainPath := filepath.Join(params.DataDir, "blockchain")
	if _, err := os.Stat(blockchainPath); os.IsNotExist(err) {
		return fmt.Errorf("blockchain directory not found at %s", blockchainPath)
	}

	// Ensure target doesn't already have data
	if info, err := os.Stat(params.TargetPath); err == nil && info.IsDir() {
		entries, err := os.ReadDir(params.TargetPath)
		if err == nil && len(entries) > 0 {
			return fmt.Errorf("target path %s already exists and is not empty", params.TargetPath)
		}
	}

	// Ensure target is not the same as source trie
	absTarget, _ := filepath.Abs(params.TargetPath)
	absSource, _ := filepath.Abs(filepath.Join(params.DataDir, "trie"))

	if absTarget == absSource {
		return fmt.Errorf("target path cannot be the same as source trie path")
	}

	return nil
}

func runPrune() (*PruneTrieResult, error) {
	triePath := filepath.Join(params.DataDir, "trie")
	blockchainPath := filepath.Join(params.DataDir, "blockchain")

	// Open blockchain storage to resolve state root
	chainStorage, err := leveldb2.NewLevelDBStorage(blockchainPath, hclog.NewNullLogger())
	if err != nil {
		return nil, fmt.Errorf("failed to open blockchain storage: %w", err)
	}
	defer chainStorage.Close()

	// Resolve state root
	var (
		stateRoot types.Hash
		blockNum  uint64
	)

	if params.BlockNum > 0 {
		stateRoot, err = GetStateRootAtBlock(chainStorage, params.BlockNum)
		if err != nil {
			return nil, err
		}

		blockNum = params.BlockNum
	} else {
		stateRoot, blockNum, err = GetLatestStateRoot(chainStorage)
		if err != nil {
			return nil, err
		}
	}

	// Open source trie read-only
	srcDB, err := leveldb.OpenFile(triePath, &opt.Options{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to open source trie (read-only): %w", err)
	}
	defer srcDB.Close()

	// Open target trie for writing
	dstDB, err := leveldb.OpenFile(params.TargetPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open target trie: %w", err)
	}
	defer dstDB.Close()

	// Get source size before pruning
	srcSize, _ := itrie.DiskSizeBytes(triePath)

	// Run prune
	result, err := itrie.PruneTrie(stateRoot, srcDB, dstDB)
	if err != nil {
		return nil, fmt.Errorf("prune failed: %w", err)
	}

	result.BlockNum = blockNum

	// Get dest size after pruning
	dstSize, _ := itrie.DiskSizeBytes(params.TargetPath)
	result.SourceSize = srcSize
	result.DestSize = dstSize

	return &PruneTrieResult{
		BlockNum:   blockNum,
		StateRoot:  stateRoot.String(),
		SourceKeys: result.SourceKeys,
		DestKeys:   result.DestKeys,
		SourceSize: srcSize,
		DestSize:   dstSize,
		Duration:   result.Duration.String(),
		Validated:  result.Validated,
	}, nil
}

// PruneTrieResult is the CLI output format
type PruneTrieResult struct {
	BlockNum   uint64 `json:"block_number"`
	StateRoot  string `json:"state_root"`
	SourceKeys int64  `json:"source_keys"`
	DestKeys   int64  `json:"dest_keys"`
	SourceSize int64  `json:"source_size_bytes"`
	DestSize   int64  `json:"dest_size_bytes"`
	Duration   string `json:"duration"`
	Validated  bool   `json:"validated"`
}

func (r *PruneTrieResult) GetOutput() string {
	var buffer bytes.Buffer

	buffer.WriteString("\n[TRIE PRUNE RESULT]\n")
	buffer.WriteString(fmt.Sprintf("Block:        %d\n", r.BlockNum))
	buffer.WriteString(fmt.Sprintf("State Root:   %s\n", r.StateRoot))
	buffer.WriteString(fmt.Sprintf("Source Keys:  %d\n", r.SourceKeys))
	buffer.WriteString(fmt.Sprintf("Dest Keys:    %d\n", r.DestKeys))
	buffer.WriteString(fmt.Sprintf("Source Size:  %s\n", formatBytes(r.SourceSize)))
	buffer.WriteString(fmt.Sprintf("Dest Size:    %s\n", formatBytes(r.DestSize)))

	if r.SourceSize > 0 {
		reduction := float64(r.SourceSize-r.DestSize) / float64(r.SourceSize) * 100
		buffer.WriteString(fmt.Sprintf("Reduction:    %.1f%%\n", reduction))
	}

	buffer.WriteString(fmt.Sprintf("Duration:     %s\n", r.Duration))
	buffer.WriteString(fmt.Sprintf("Validated:    %v\n", r.Validated))

	if r.Validated {
		buffer.WriteString("\nState root verified. You can now swap directories:\n")
		buffer.WriteString("  mv <data-dir>/trie <data-dir>/trie_old\n")
		buffer.WriteString("  mv <target-path> <data-dir>/trie\n")
	}

	return buffer.String()
}

func formatBytes(b int64) string {
	const unit = 1024

	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := int64(unit), 0

	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
