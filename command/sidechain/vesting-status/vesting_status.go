package vestingstatus

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/command/polybftsecrets"
	"github.com/0xPolygon/polygon-edge/command/sidechain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/txrelayer"
	"github.com/spf13/cobra"
	"github.com/umbracle/ethgo"
	"github.com/umbracle/ethgo/abi"
)

var (
	params vestingStatusParams

	vestedStakingPositionsFn       = contractsapi.HydraStaking.Abi.Methods["vestedStakingPositions"]
	calculatePositionTotalRewardFn = contractsapi.HydraStaking.Abi.Methods["calculatePositionTotalReward"]
	unclaimedRewardsFn             = contractsapi.HydraStaking.Abi.Methods["unclaimedRewards"]
	distributedCommissionsFn       = contractsapi.HydraDelegation.Abi.Methods["distributedCommissions"]
)

func GetCommand() *cobra.Command {
	vestingStatusCmd := &cobra.Command{
		Use:     "vesting-status",
		Short:   "Displays the vesting status of a validator, including vesting period, rewards, and commissions",
		PreRunE: runPreRun,
		RunE:    runCommand,
	}

	setFlags(vestingStatusCmd)

	return vestingStatusCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.accountDir,
		polybftsecrets.AccountDirFlag,
		"",
		polybftsecrets.AccountDirFlagDesc,
	)

	cmd.Flags().StringVar(
		&params.accountConfig,
		polybftsecrets.AccountConfigFlag,
		"",
		polybftsecrets.AccountConfigFlagDesc,
	)

	cmd.Flags().BoolVar(
		&params.insecureLocalStore,
		sidechain.InsecureLocalStoreFlag,
		false,
		"a flag to indicate if the secrets used are encrypted. If set to true, the secrets are stored in plain text.",
	)

	helper.RegisterJSONRPCFlag(cmd)

	cmd.MarkFlagsMutuallyExclusive(polybftsecrets.AccountDirFlag, polybftsecrets.AccountConfigFlag)
}

func runPreRun(cmd *cobra.Command, _ []string) error {
	params.jsonRPC = helper.GetJSONRPCAddress(cmd)

	return params.validateFlags()
}

func runCommand(cmd *cobra.Command, _ []string) error {
	outputter := command.InitializeOutputter(cmd)
	defer outputter.WriteOutput()

	validatorAccount, err := sidechain.GetAccount(params.accountDir, params.accountConfig, params.insecureLocalStore)
	if err != nil {
		return err
	}

	validatorAddr := validatorAccount.Ecdsa.Address()

	txRelayer, err := txrelayer.NewTxRelayer(
		txrelayer.WithIPAddress(params.jsonRPC),
	)
	if err != nil {
		return err
	}

	result, err := getVestingStatus(txRelayer, validatorAddr)
	if err != nil {
		return err
	}

	outputter.WriteCommandResult(result)

	return nil
}

// getVestingStatus queries all vesting-related contract state for a validator and returns the result.
func getVestingStatus(txRelayer txrelayer.TxRelayer, validatorAddr ethgo.Address) (*vestingStatusResult, error) {
	result := &vestingStatusResult{
		ValidatorAddress: validatorAddr.String(),
	}

	// 1. Query vestedStakingPositions from HydraStaking
	vestingPosition, err := queryVestedStakingPositions(txRelayer, validatorAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to query vesting position: %w", err)
	}

	duration := vestingPosition["duration"].(*big.Int)     //nolint:forcetypeassert
	start := vestingPosition["start"].(*big.Int)           //nolint:forcetypeassert
	end := vestingPosition["end"].(*big.Int)               //nolint:forcetypeassert
	base := vestingPosition["base"].(*big.Int)             //nolint:forcetypeassert
	vestBonus := vestingPosition["vestBonus"].(*big.Int)   //nolint:forcetypeassert
	rsiBonus := vestingPosition["rsiBonus"].(*big.Int)     //nolint:forcetypeassert
	commission := vestingPosition["commission"].(*big.Int) //nolint:forcetypeassert

	result.IsActiveVestingPosition = start.Sign() > 0
	result.VestingDuration = duration.String()
	result.VestingStart = formatTimestamp(start)
	result.VestingEnd = formatTimestamp(end)
	result.BaseStake = formatWei(base)
	result.VestBonus = formatWei(vestBonus)
	result.RSIBonus = formatWei(rsiBonus)
	result.VestingCommission = commission.String()

	// 2. Query calculatePositionTotalReward from HydraStaking
	totalReward, err := querySingleUint256(
		txRelayer, validatorAddr,
		calculatePositionTotalRewardFn,
		(ethgo.Address)(contracts.HydraStakingContract),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query total reward: %w", err)
	}

	result.GeneratedRewards = formatWei(totalReward)

	// 3. Query unclaimedRewards from HydraStaking
	unclaimed, err := querySingleUint256(
		txRelayer, validatorAddr,
		unclaimedRewardsFn,
		(ethgo.Address)(contracts.HydraStakingContract),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query unclaimed rewards: %w", err)
	}

	result.ClaimableRewards = formatWei(unclaimed)

	// 4. Query distributedCommissions from HydraDelegation
	// Note: distributedCommissions returns the currently claimable (pending) commissions,
	// not the total historical commissions ever distributed.
	commissions, err := querySingleUint256(
		txRelayer, validatorAddr,
		distributedCommissionsFn,
		(ethgo.Address)(contracts.HydraDelegationContract),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query distributed commissions: %w", err)
	}

	result.ClaimableCommissions = formatWei(commissions)

	return result, nil
}

// queryVestedStakingPositions calls the vestedStakingPositions view function on HydraStaking
func queryVestedStakingPositions(
	txRelayer txrelayer.TxRelayer, validatorAddr ethgo.Address,
) (map[string]interface{}, error) {
	encoded, err := vestedStakingPositionsFn.Encode([]interface{}{validatorAddr})
	if err != nil {
		return nil, fmt.Errorf("failed to encode vestedStakingPositions call: %w", err)
	}

	response, err := txRelayer.Call(validatorAddr, (ethgo.Address)(contracts.HydraStakingContract), encoded)
	if err != nil {
		return nil, err
	}

	byteResponse, err := hex.DecodeHex(response)
	if err != nil {
		return nil, fmt.Errorf("unable to decode hex response: %w", err)
	}

	decoded, err := vestedStakingPositionsFn.Outputs.Decode(byteResponse)
	if err != nil {
		return nil, err
	}

	decodedMap, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("could not convert decoded outputs to map")
	}

	return decodedMap, nil
}

// querySingleUint256 calls a view function that takes a single address and returns a single uint256
func querySingleUint256(
	txRelayer txrelayer.TxRelayer,
	validatorAddr ethgo.Address,
	method *abi.Method,
	contractAddr ethgo.Address,
) (*big.Int, error) {
	encoded, err := method.Encode([]interface{}{validatorAddr})
	if err != nil {
		return nil, fmt.Errorf("failed to encode %s call: %w", method.Name, err)
	}

	response, err := txRelayer.Call(validatorAddr, contractAddr, encoded)
	if err != nil {
		return nil, err
	}

	byteResponse, err := hex.DecodeHex(response)
	if err != nil {
		return nil, fmt.Errorf("unable to decode hex response: %w", err)
	}

	decoded, err := method.Outputs.Decode(byteResponse)
	if err != nil {
		return nil, err
	}

	decodedMap, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("could not convert decoded outputs to map")
	}

	value, ok := decodedMap["0"].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("could not convert output to big.Int")
	}

	return value, nil
}
