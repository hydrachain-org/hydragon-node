package publicconfigs

import (
	_ "embed"
)

//go:embed genesis-mainnet.json
var mainnetConfig []byte

//go:embed genesis-testnet.json
var testnetConfig []byte

func GetMainnetGenesis() []byte {
	return mainnetConfig
}

func GetTestnetGenesis() []byte {
	return testnetConfig
}
