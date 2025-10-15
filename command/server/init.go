package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"

	"github.com/0xPolygon/polygon-edge/command/server/config"

	helperCommon "github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/network/common"

	"github.com/0xPolygon/polygon-edge/chain"
	publicconfigs "github.com/0xPolygon/polygon-edge/chain/public-configs"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/network"
	"github.com/0xPolygon/polygon-edge/secrets"
	"github.com/0xPolygon/polygon-edge/server"
)

var (
	errDataDirectoryUndefined = errors.New("data directory not defined")

	// Network constants
	mainnetNetwork = "mainnet"
	testnetNetwork = "testnet"
)

func (p *serverParams) initConfigFromFile() error {
	var parseErr error

	if p.rawConfig, parseErr = config.ReadConfigFile(p.configPath); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initRawParams() error {
	if err := p.initBlockGasTarget(); err != nil {
		return err
	}

	if err := p.initSecretsConfig(); err != nil {
		return err
	}

	if err := p.initGenesisConfig(); err != nil {
		return err
	}

	if err := p.initBootnodeConfig(); err != nil {
		return err
	}

	if err := p.initDataDirLocation(); err != nil {
		return err
	}

	if p.isDevMode {
		p.initDevMode()
	}

	p.initPeerLimits()
	p.initLogFileLocation()

	p.relayer = p.rawConfig.Relayer

	return p.initAddresses()
}

func (p *serverParams) initDataDirLocation() error {
	if p.rawConfig.DataDir == "" {
		return errDataDirectoryUndefined
	}

	return nil
}

func (p *serverParams) initLogFileLocation() {
	if p.isLogFileLocationSet() {
		p.logFileLocation = p.rawConfig.LogFilePath
	}
}

func (p *serverParams) initBlockGasTarget() error {
	var parseErr error

	if p.blockGasTarget, parseErr = helperCommon.ParseUint64orHex(
		&p.rawConfig.BlockGasTarget,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initSecretsConfig() error {
	if !p.isSecretsConfigPathSet() {
		return nil
	}

	var parseErr error

	if p.secretsConfig, parseErr = secrets.ReadConfig(
		p.rawConfig.SecretsConfigPath,
	); parseErr != nil {
		return fmt.Errorf("unable to read secrets config file, %w", parseErr)
	}

	return nil
}

func (p *serverParams) initGenesisConfig() error {
	var parseErr error

	if p.genesisConfig, parseErr = chain.Import(
		p.rawConfig.GenesisFile,
	); parseErr != nil {
		return parseErr
	}

	// if block-gas-target flag is set override genesis.json value
	if p.blockGasTarget != 0 {
		p.genesisConfig.Params.BlockGasTarget = p.blockGasTarget
	}

	return nil
}

func (p *serverParams) initDevMode() {
	// Dev mode:
	// - disables peer discovery
	// - enables all forks
	p.rawConfig.Network.NoDiscover = true
	p.genesisConfig.Params.Forks = chain.AllForksEnabled

	p.initDevConsensusConfig()
}

func (p *serverParams) initDevConsensusConfig() {
	if !p.isDevConsensus() {
		return
	}

	p.genesisConfig.Params.Engine = map[string]interface{}{
		string(server.DevConsensus): map[string]interface{}{
			"interval": p.devInterval,
		},
	}
}

func (p *serverParams) initPeerLimits() {
	if !p.isMaxPeersSet() && !p.isPeerRangeSet() {
		// No peer limits specified, use the default limits
		p.initDefaultPeerLimits()

		return
	}

	if p.isPeerRangeSet() {
		// Some part of the peer range is specified
		p.initUsingPeerRange()

		return
	}

	if p.isMaxPeersSet() {
		// The max peer value is specified, derive precise limits
		p.initUsingMaxPeers()

		return
	}
}

func (p *serverParams) initDefaultPeerLimits() {
	defaultNetworkConfig := network.DefaultConfig()

	p.rawConfig.Network.MaxPeers = defaultNetworkConfig.MaxPeers
	p.rawConfig.Network.MaxInboundPeers = defaultNetworkConfig.MaxInboundPeers
	p.rawConfig.Network.MaxOutboundPeers = defaultNetworkConfig.MaxOutboundPeers
}

func (p *serverParams) initUsingPeerRange() {
	defaultConfig := network.DefaultConfig()

	if p.rawConfig.Network.MaxInboundPeers == unsetPeersValue {
		p.rawConfig.Network.MaxInboundPeers = defaultConfig.MaxInboundPeers
	}

	if p.rawConfig.Network.MaxOutboundPeers == unsetPeersValue {
		p.rawConfig.Network.MaxOutboundPeers = defaultConfig.MaxOutboundPeers
	}

	p.rawConfig.Network.MaxPeers = p.rawConfig.Network.MaxInboundPeers + p.rawConfig.Network.MaxOutboundPeers
}

func (p *serverParams) initUsingMaxPeers() {
	p.rawConfig.Network.MaxOutboundPeers = int64(
		math.Floor(
			float64(p.rawConfig.Network.MaxPeers) * network.DefaultDialRatio,
		),
	)
	// MaxPeers is expected to be greater than MaxOutboundPeers as long as DefaultDialRatio is less than 0
	if p.rawConfig.Network.MaxPeers > p.rawConfig.Network.MaxOutboundPeers {
		p.rawConfig.Network.MaxInboundPeers = p.rawConfig.Network.MaxPeers - p.rawConfig.Network.MaxOutboundPeers
	}
}

func (p *serverParams) initAddresses() error {
	if err := p.initPrometheusAddress(); err != nil {
		return err
	}

	if err := p.initLibp2pAddress(); err != nil {
		return err
	}

	if err := p.initNATAddress(); err != nil {
		return err
	}

	if err := p.initDNSAddress(); err != nil {
		return err
	}

	if err := p.initJSONRPCAddress(); err != nil {
		return err
	}

	return p.initGRPCAddress()
}

func (p *serverParams) initPrometheusAddress() error {
	if !p.isPrometheusAddressSet() {
		return nil
	}

	var parseErr error

	if p.prometheusAddress, parseErr = helper.ResolveAddr(
		p.rawConfig.Telemetry.PrometheusAddr,
		helper.AllInterfacesBinding,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initLibp2pAddress() error {
	var parseErr error

	if p.libp2pAddress, parseErr = helper.ResolveAddr(
		p.rawConfig.Network.Libp2pAddr,
		helper.LocalHostBinding,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initNATAddress() error {
	if !p.isNATAddressSet() {
		return nil
	}

	if p.natAddress = net.ParseIP(
		p.rawConfig.Network.NatAddr,
	); p.natAddress == nil {
		return errInvalidNATAddress
	}

	return nil
}

func (p *serverParams) initDNSAddress() error {
	if !p.isDNSAddressSet() {
		return nil
	}

	var parseErr error

	if p.dnsAddress, parseErr = common.MultiAddrFromDNS(
		p.rawConfig.Network.DNSAddr, p.libp2pAddress.Port,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initJSONRPCAddress() error {
	var parseErr error

	if p.jsonRPCAddress, parseErr = helper.ResolveAddr(
		p.rawConfig.JSONRPCAddr,
		helper.AllInterfacesBinding,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

func (p *serverParams) initGRPCAddress() error {
	var parseErr error

	if p.grpcAddress, parseErr = helper.ResolveAddr(
		p.rawConfig.GRPCAddr,
		helper.LocalHostBinding,
	); parseErr != nil {
		return parseErr
	}

	return nil
}

// initBootnodeConfig initializes the bootnode configuration from various sources
func (p *serverParams) initBootnodeConfig() error {
	var err error
	p.bootnodeConfig, err = p.getBootnodeConfig()

	return err
}

// getBootnodeConfig retrieves bootnode configuration from various sources
func (p *serverParams) getBootnodeConfig() (*chain.Bootnode, error) {
	var defaultBootnodes []string

	var userBootnodes []string

	// 1. Load default bootnodes (mainnet/testnet or bootnode.json)
	if p.rawConfig.GenesisFile == mainnetNetwork || p.rawConfig.GenesisFile == testnetNetwork {
		// The bootnodes will be loaded from the chain config for mainnet/testnet
		p.logger.Info("Using default bootnodes for", "network", p.rawConfig.GenesisFile)

		if p.genesisConfig != nil && p.genesisConfig.Params != nil {
			defaultBootnodes = p.genesisConfig.Params.Bootnodes

			// Also parse top-level bootnodes from embedded genesis JSON
			var topLevel struct {
				Bootnodes []string `json:"bootnodes"`
			}

			var embed []byte
			if p.rawConfig.GenesisFile == mainnetNetwork {
				embed = publicconfigs.GetMainnetGenesis()
			} else {
				embed = publicconfigs.GetTestnetGenesis()
			}

			if err := json.Unmarshal(embed, &topLevel); err == nil && len(topLevel.Bootnodes) > 0 {
				defaultBootnodes = append(defaultBootnodes, topLevel.Bootnodes...)
			}
		}
		// Also support adjacent geth-style bootnode.json for mainnet/testnet
		// Try a couple of common locations: current working directory and data-dir
		candidatePaths := []string{
			"bootnode.json",
			filepath.Join(p.rawConfig.DataDir, "bootnode.json"),
		}
		for _, configPath := range candidatePaths {
			if _, err := os.Stat(configPath); err == nil {
				if data, err := os.ReadFile(configPath); err == nil {
					var nodeConfig struct {
						Node struct {
							P2P struct {
								StaticNodes []string `json:"staticNodes"`
							} `json:"p2p"`
						} `json:"node"`
					}

					if err := json.Unmarshal(data, &nodeConfig); err == nil && len(nodeConfig.Node.P2P.StaticNodes) > 0 {
						defaultBootnodes = append(defaultBootnodes, nodeConfig.Node.P2P.StaticNodes...)
					}
				}
			}
		}
	} else {
		// For custom networks, first load bootnodes from genesis params if present
		if p.genesisConfig != nil && p.genesisConfig.Params != nil && len(p.genesisConfig.Params.Bootnodes) > 0 {
			defaultBootnodes = append(defaultBootnodes, p.genesisConfig.Params.Bootnodes...)
		}

		// Also parse top-level bootnodes directly from the custom genesis JSON
		if p.rawConfig.GenesisFile != "" {
			if data, err := os.ReadFile(p.rawConfig.GenesisFile); err == nil {
				var topLevel struct {
					Bootnodes []string `json:"bootnodes"`
				}

				if err := json.Unmarshal(data, &topLevel); err == nil && len(topLevel.Bootnodes) > 0 {
					defaultBootnodes = append(defaultBootnodes, topLevel.Bootnodes...)
				}
			}
		}

		// Then try to load additional bootnodes from adjacent bootnode.json
		candidatePaths := []string{
			filepath.Join(filepath.Dir(p.rawConfig.GenesisFile), "bootnode.json"),
			// also check current directory and data dir for convenience
			"bootnode.json",
			filepath.Join(p.rawConfig.DataDir, "bootnode.json"),
		}
		for _, configPath := range candidatePaths {
			if _, err := os.Stat(configPath); err == nil {
				if data, err := os.ReadFile(configPath); err == nil {
					var nodeConfig struct {
						Node struct {
							P2P struct {
								StaticNodes []string `json:"staticNodes"`
							} `json:"p2p"`
						} `json:"node"`
					}

					if err := json.Unmarshal(data, &nodeConfig); err == nil && len(nodeConfig.Node.P2P.StaticNodes) > 0 {
						defaultBootnodes = append(defaultBootnodes, nodeConfig.Node.P2P.StaticNodes...)
					}
				}
			}
		}
	}

	// 2. Load user-specified bootnodes from bootnodePath, if set
	if p.rawConfig.BootnodePath != "" {
		data, err := os.ReadFile(p.rawConfig.BootnodePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read bootnode config file %s: %w", p.rawConfig.BootnodePath, err)
		}

		// Accept only geth-style schema: { "node": { "p2p": { "staticNodes": [ ... ] } } }
		var gethStyle struct {
			Node struct {
				P2P struct {
					StaticNodes []string `json:"staticNodes"`
				} `json:"p2p"`
			} `json:"node"`
		}

		if err := json.Unmarshal(data, &gethStyle); err == nil && len(gethStyle.Node.P2P.StaticNodes) > 0 {
			userBootnodes = gethStyle.Node.P2P.StaticNodes
		} else {
			return nil, fmt.Errorf(
				"failed to parse bootnode config file %s: expected geth-style node.p2p.staticNodes",
				p.rawConfig.BootnodePath,
			)
		}
	}

	// 3. Merge default and user bootnodes, removing duplicates
	bootnodeSet := make(map[string]struct{})
	allBootnodes := make([]string, 0)

	for _, b := range defaultBootnodes {
		if _, exists := bootnodeSet[b]; !exists {
			bootnodeSet[b] = struct{}{}

			allBootnodes = append(allBootnodes, b)
		}
	}

	for _, b := range userBootnodes {
		if _, exists := bootnodeSet[b]; !exists {
			bootnodeSet[b] = struct{}{}

			allBootnodes = append(allBootnodes, b)
		}
	}

	// 4. Fallback: If still empty, try to load last_peers.json
	if len(allBootnodes) == 0 {
		lastPeersPath := filepath.Join(p.rawConfig.DataDir, "libp2p", "last_peers.json")
		if data, err := os.ReadFile(lastPeersPath); err == nil {
			var lastPeers []string
			if err := json.Unmarshal(data, &lastPeers); err == nil {
				for _, b := range lastPeers {
					bootnodeSet[b] = struct{}{}
				}

				p.logger.Info(fmt.Sprintf("Loaded bootnodes from last_peers.json: %v", lastPeers))
			}
		}
		// Rebuild bootnodes slice
		allBootnodes = make([]string, 0, len(bootnodeSet))
		for b := range bootnodeSet {
			allBootnodes = append(allBootnodes, b)
		}
	}

	// 5. Persist bootnodes to geth-style bootnode.json if not present yet
	if len(allBootnodes) > 0 {
		// Prepare geth-style schema
		type p2pCfg struct {
			StaticNodes []string `json:"staticNodes"`
		}

		type nodeCfg struct {
			P2P p2pCfg `json:"p2p"`
		}

		type outCfg struct {
			Node nodeCfg `json:"node"`
			JSON struct {
				Enabled bool `json:"enabled"`
			} `json:"json"`
		}

		writeIfMissing := func(path string) {
			if path == "" {
				return
			}

			if _, err := os.Stat(path); err == nil {
				return
			}

			cfg := outCfg{}
			cfg.Node.P2P.StaticNodes = allBootnodes
			cfg.JSON.Enabled = false

			if f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644); err == nil {
				enc := json.NewEncoder(f)
				enc.SetIndent("", "  ")
				_ = enc.Encode(&cfg)
				_ = f.Close()
			}
		}

		// Always try to place in data dir
		writeIfMissing(filepath.Join(p.rawConfig.DataDir, "bootnode.json"))

		// For custom genesis, also try adjacent to the genesis file
		if p.rawConfig.GenesisFile != "" &&
			p.rawConfig.GenesisFile != mainnetNetwork &&
			p.rawConfig.GenesisFile != testnetNetwork {
			writeIfMissing(filepath.Join(filepath.Dir(p.rawConfig.GenesisFile), "bootnode.json"))
		}
	}

	if len(allBootnodes) == 0 {
		return nil, fmt.Errorf("no bootnodes found. Provide them via genesis params.bootnodes, " +
			"adjacent bootnode.json, or --bootnodes <./path/to/bootnode.json> or use mainnet/testnet")
	}

	return &chain.Bootnode{
		Bootnodes: allBootnodes,
	}, nil
}
