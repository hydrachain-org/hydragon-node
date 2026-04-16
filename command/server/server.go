package server

import (
	"fmt"

	"os"
	"path/filepath"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/command/server/config"
	"github.com/0xPolygon/polygon-edge/command/server/export"
	"github.com/0xPolygon/polygon-edge/server"
	"github.com/spf13/cobra"

	leveldb2 "github.com/0xPolygon/polygon-edge/blockchain/storage/leveldb"
	pruneCmd "github.com/0xPolygon/polygon-edge/command/prune"
	itrie "github.com/0xPolygon/polygon-edge/state/immutable-trie"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

func GetCommand() *cobra.Command {
	serverCmd := &cobra.Command{
		Use:     "server",
		Short:   "The default command that starts the Hydra Chain client, by bootstrapping all modules together",
		PreRunE: runPreRun,
		Run:     runCommand,
	}

	helper.RegisterGRPCAddressFlag(serverCmd)
	helper.RegisterLegacyGRPCAddressFlag(serverCmd)
	helper.RegisterJSONRPCFlag(serverCmd)

	registerSubcommands(serverCmd)
	setFlags(serverCmd)

	return serverCmd
}

func registerSubcommands(baseCmd *cobra.Command) {
	baseCmd.AddCommand(
		// server export
		export.GetCommand(),
	)
}

func setFlags(cmd *cobra.Command) {
	defaultConfig := config.DefaultConfig()

	cmd.Flags().StringVar(
		&params.rawConfig.LogLevel,
		command.LogLevelFlag,
		defaultConfig.LogLevel,
		"the log level for console output",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.GenesisFile,
		genesisFlag,
		defaultConfig.GenesisFile,
		"the genesis file used for starting the chain."+
			`Can be "mainnet", "testnet" or "custom"<path_to_custom_genesis_file>"`,
	)

	cmd.Flags().StringVar(
		&params.rawConfig.BootnodePath,
		bootnodePathFlag,
		defaultConfig.BootnodePath,
		"the bootnode file used for connecting to chain",
	)
	cmd.Flags().StringVar(
		&params.configPath,
		configFlag,
		"",
		"the path to the CLI config. Supports .json, .hcl, .yaml, .yml",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.DataDir,
		dataDirFlag,
		defaultConfig.DataDir,
		"the data directory used for storing Hydra Chain client data",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.Network.Libp2pAddr,
		libp2pAddressFlag,
		defaultConfig.Network.Libp2pAddr,
		"the address and port for the libp2p service",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.Telemetry.PrometheusAddr,
		prometheusAddressFlag,
		"",
		"the address and port for the prometheus instrumentation service (address:port). "+
			"If only port is defined (:port) it will bind to 0.0.0.0:port",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.Network.NatAddr,
		natFlag,
		"",
		"the external IP address without port, as can be seen by peers",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.Network.DNSAddr,
		dnsFlag,
		"",
		"the host DNS address which can be used by a remote peer for connection",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.BlockGasTarget,
		blockGasTargetFlag,
		defaultConfig.BlockGasTarget,
		"the target block gas limit for the chain. If omitted, the value of the parent block is used",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.SecretsConfigPath,
		command.SecretsConfigFlag,
		command.DefaultSecretsConfigPath,
		command.DefaultSecretsConfigPathDesc,
	)

	cmd.Flags().StringVar(
		&params.rawConfig.RestoreFile,
		restoreFlag,
		"",
		"the path to the archive blockchain data to restore on initialization",
	)

	cmd.Flags().BoolVar(
		&params.rawConfig.ShouldSeal,
		sealFlag,
		defaultConfig.ShouldSeal,
		"the flag indicating that the client should seal blocks",
	)

	cmd.Flags().BoolVar(
		&params.rawConfig.Network.NoDiscover,
		command.NoDiscoverFlag,
		defaultConfig.Network.NoDiscover,
		"prevent the client from discovering other peers",
	)

	cmd.Flags().Int64Var(
		&params.rawConfig.Network.MaxPeers,
		maxPeersFlag,
		-1,
		"the client's max number of peers allowed",
	)
	// override default usage value
	cmd.Flag(maxPeersFlag).DefValue = fmt.Sprintf("%d", defaultConfig.Network.MaxPeers)

	cmd.Flags().Int64Var(
		&params.rawConfig.Network.MaxInboundPeers,
		maxInboundPeersFlag,
		-1,
		"the client's max number of inbound peers allowed",
	)
	// override default usage value
	cmd.Flag(maxInboundPeersFlag).DefValue = fmt.Sprintf(
		"%d",
		defaultConfig.Network.MaxInboundPeers,
	)
	cmd.MarkFlagsMutuallyExclusive(maxPeersFlag, maxInboundPeersFlag)

	cmd.Flags().Int64Var(
		&params.rawConfig.Network.MaxOutboundPeers,
		maxOutboundPeersFlag,
		-1,
		"the client's max number of outbound peers allowed",
	)
	// override default usage value
	cmd.Flag(maxOutboundPeersFlag).DefValue = fmt.Sprintf(
		"%d",
		defaultConfig.Network.MaxOutboundPeers,
	)
	cmd.MarkFlagsMutuallyExclusive(maxPeersFlag, maxOutboundPeersFlag)

	cmd.Flags().Uint64Var(
		&params.rawConfig.TxPool.PriceLimit,
		priceLimitFlag,
		defaultConfig.TxPool.PriceLimit,
		fmt.Sprintf(
			"the minimum gas price limit to enforce for acceptance into the pool (default %d)",
			defaultConfig.TxPool.PriceLimit,
		),
	)

	cmd.Flags().Uint64Var(
		&params.rawConfig.TxPool.MaxSlots,
		maxSlotsFlag,
		defaultConfig.TxPool.MaxSlots,
		"maximum slots in the pool",
	)

	cmd.Flags().Uint64Var(
		&params.rawConfig.TxPool.MaxAccountEnqueued,
		maxEnqueuedFlag,
		defaultConfig.TxPool.MaxAccountEnqueued,
		"maximum number of enqueued transactions per account",
	)

	cmd.Flags().StringArrayVar(
		&params.rawConfig.CorsAllowedOrigins,
		corsOriginFlag,
		defaultConfig.Headers.AccessControlAllowOrigins,
		"the CORS header indicating whether any JSON-RPC response can be shared with the specified origin",
	)

	cmd.Flags().Uint64Var(
		&params.rawConfig.JSONRPCBatchRequestLimit,
		jsonRPCBatchRequestLimitFlag,
		defaultConfig.JSONRPCBatchRequestLimit,
		"max length to be considered when handling json-rpc batch requests, value of 0 disables it",
	)

	cmd.Flags().Uint64Var(
		&params.rawConfig.JSONRPCBlockRangeLimit,
		jsonRPCBlockRangeLimitFlag,
		defaultConfig.JSONRPCBlockRangeLimit,
		"max block range to be considered when executing json-rpc requests "+
			"that consider fromBlock/toBlock values (e.g. eth_getLogs), value of 0 disables it",
	)

	cmd.Flags().StringVar(
		&params.rawConfig.LogFilePath,
		logFileLocationFlag,
		defaultConfig.LogFilePath,
		"write all logs to the file at specified location instead of writing them to console",
	)

	cmd.Flags().Uint64Var(
		&params.rawConfig.NumBlockConfirmations,
		numBlockConfirmationsFlag,
		defaultConfig.NumBlockConfirmations,
		"minimal number of child blocks required for the parent block to be considered final",
	)

	cmd.Flags().Uint64Var(
		&params.rawConfig.ConcurrentRequestsDebug,
		concurrentRequestsDebugFlag,
		defaultConfig.ConcurrentRequestsDebug,
		"maximal number of concurrent requests for debug endpoints",
	)

	cmd.Flags().Uint64Var(
		&params.rawConfig.WebSocketReadLimit,
		webSocketReadLimitFlag,
		defaultConfig.WebSocketReadLimit,
		"maximum size in bytes for a message read from the peer by websocket",
	)

	cmd.Flags().DurationVar(
		&params.rawConfig.MetricsInterval,
		metricsIntervalFlag,
		defaultConfig.MetricsInterval,
		"the interval (in seconds) at which special metrics are generated. a value of zero means the metrics are disabled",
	)

	cmd.Flags().BoolVar(
		&params.shouldPrune,
		pruneFlag,
		false,
		"prune historical state trie data before starting the node. "+
			"Copies only reachable nodes from the latest state root to a new trie database, "+
			"swaps directories, then starts normally. Original trie is kept as trie_old/",
	)

	setLegacyFlags(cmd)

	setDevFlags(cmd)
}

// setLegacyFlags sets the legacy flags to preserve backwards compatibility
// with running partners
func setLegacyFlags(cmd *cobra.Command) {
	// Legacy IBFT base timeout flag
	cmd.Flags().Uint64Var(
		&params.ibftBaseTimeoutLegacy,
		ibftBaseTimeoutFlagLEGACY,
		0,
		"",
	)

	_ = cmd.Flags().MarkHidden(ibftBaseTimeoutFlagLEGACY)
}

func setDevFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(
		&params.isDevMode,
		devFlag,
		false,
		"should the client start in dev mode (default false)",
	)

	_ = cmd.Flags().MarkHidden(devFlag)

	cmd.Flags().Uint64Var(
		&params.devInterval,
		devIntervalFlag,
		0,
		"the client's dev notification interval in seconds (default 1)",
	)

	_ = cmd.Flags().MarkHidden(devIntervalFlag)
}

func runPreRun(cmd *cobra.Command, _ []string) error {
	// Set the grpc and json ip:port bindings
	// The config file will have precedence over --flag
	params.setRawGRPCAddress(helper.GetGRPCAddress(cmd))
	params.setRawJSONRPCAddress(helper.GetJSONRPCAddress(cmd))
	params.setJSONLogFormat(helper.GetJSONLogFormat(cmd))

	// Check if the config file has been specified
	// Config file settings will override JSON-RPC and GRPC address values
	if isConfigFileSpecified(cmd) {
		if err := params.initConfigFromFile(); err != nil {
			return err
		}
	}

	// Before raw params are initialized, set the actual genesis path (if custom) based on --chain flag
	if err := params.setGenesisFileFlag(params.rawConfig.GenesisFile); err != nil {
		return err
	}

	if err := params.initRawParams(); err != nil {
		return err
	}

	return nil
}

func isConfigFileSpecified(cmd *cobra.Command) bool {
	return cmd.Flags().Changed(configFlag)
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	// Pre-startup prune if --prune flag is set
	if params.shouldPrune {
		if err := runPreStartupPrune(outputter); err != nil {
			outputter.SetError(fmt.Errorf("pre-startup prune failed (original trie untouched): %w", err))
			outputter.WriteOutput()

			return
		}
	}

	config, err := params.generateConfig()
	if err != nil {
		outputter.SetError(err)
		outputter.WriteOutput()

		return
	}

	if err := runServerLoop(config, outputter); err != nil {
		outputter.SetError(err)
		outputter.WriteOutput()

		return
	}
}

func runPreStartupPrune(outputter command.OutputFormatter) error {
	dataDir := params.rawConfig.DataDir
	triePath := filepath.Join(dataDir, "trie")
	blockchainPath := filepath.Join(dataDir, "blockchain")
	targetPath := filepath.Join(dataDir, "trie_new")

	// Verify paths exist
	if _, err := os.Stat(triePath); os.IsNotExist(err) {
		return fmt.Errorf("trie directory not found at %s", triePath)
	}

	if _, err := os.Stat(blockchainPath); os.IsNotExist(err) {
		return fmt.Errorf("blockchain directory not found at %s", blockchainPath)
	}

	// Don't prune if trie_new already exists (interrupted previous prune)
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("target %s already exists — previous prune may have been interrupted. "+
			"Remove it manually before retrying", targetPath)
	}

	logger := hclog.New(&hclog.LoggerOptions{
		Name:  "prune",
		Level: hclog.Info,
	})

	logger.Info("Starting pre-startup trie prune", "data-dir", dataDir)

	// Resolve state root from blockchain DB
	chainStorage, err := leveldb2.NewLevelDBStorage(blockchainPath, logger)
	if err != nil {
		return fmt.Errorf("failed to open blockchain storage: %w", err)
	}

	stateRoot, blockNum, err := pruneCmd.GetLatestStateRoot(chainStorage)
	if err != nil {
		chainStorage.Close()

		return fmt.Errorf("failed to resolve state root: %w", err)
	}

	chainStorage.Close()

	logger.Info("State root resolved", "block", blockNum, "root", stateRoot.String())

	// Open source read-only
	srcDB, err := leveldb.OpenFile(triePath, &opt.Options{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("failed to open source trie: %w", err)
	}

	// Open target for writing
	dstDB, err := leveldb.OpenFile(targetPath, nil)
	if err != nil {
		srcDB.Close()

		return fmt.Errorf("failed to open target trie: %w", err)
	}

	// Run prune
	result, err := itrie.PruneTrie(stateRoot, srcDB, dstDB)

	srcDB.Close()
	dstDB.Close()

	if err != nil {
		// Clean up failed target
		os.RemoveAll(targetPath)

		return fmt.Errorf("prune failed: %w", err)
	}

	logger.Info("Prune completed",
		"source_keys", result.SourceKeys,
		"dest_keys", result.DestKeys,
		"duration", result.Duration.String(),
		"validated", result.Validated,
	)

	// Swap directories: trie → trie_old, trie_new → trie
	trieOldPath := filepath.Join(dataDir, "trie_old")

	// Remove any existing trie_old from a previous prune
	os.RemoveAll(trieOldPath)

	// Preserve original ownership/permissions
	srcInfo, err := os.Stat(triePath)
	if err != nil {
		return fmt.Errorf("failed to stat source trie: %w", err)
	}

	if err := os.Rename(triePath, trieOldPath); err != nil {
		return fmt.Errorf("failed to rename trie → trie_old: %w", err)
	}

	if err := os.Rename(targetPath, triePath); err != nil {
		// Rollback: move trie_old back to trie
		os.Rename(trieOldPath, triePath)

		return fmt.Errorf("failed to rename trie_new → trie: %w", err)
	}

	// Match permissions of the new trie to the original
	os.Chmod(triePath, srcInfo.Mode())

	srcSize, _ := itrie.DiskSizeBytes(trieOldPath)
	dstSize, _ := itrie.DiskSizeBytes(triePath)

	reduction := float64(0)
	if srcSize > 0 {
		reduction = float64(srcSize-dstSize) / float64(srcSize) * 100
	}

	logger.Info("Trie swap complete",
		"old_size", fmt.Sprintf("%.1f MB", float64(srcSize)/1048576),
		"new_size", fmt.Sprintf("%.1f MB", float64(dstSize)/1048576),
		"reduction", fmt.Sprintf("%.1f%%", reduction),
	)

	logger.Info("Old trie preserved at trie_old/ — delete after confirming stability")

	return nil
}

func runServerLoop(
	config *server.Config,
	outputter command.OutputFormatter,
) error {
	serverInstance, err := server.NewServer(config)
	if err != nil {
		return err
	}

	return helper.HandleSignals(serverInstance.Close, outputter)
}
