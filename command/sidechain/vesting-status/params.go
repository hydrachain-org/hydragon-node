package vestingstatus

import (
	"bytes"
	"fmt"
	"math/big"
	"time"

	"github.com/0xPolygon/polygon-edge/command/helper"
	sidechainHelper "github.com/0xPolygon/polygon-edge/command/sidechain"
)

type vestingStatusParams struct {
	accountDir         string
	accountConfig      string
	jsonRPC            string
	insecureLocalStore bool
}

func (v *vestingStatusParams) validateFlags() error {
	if _, err := helper.ParseJSONRPCAddress(v.jsonRPC); err != nil {
		return fmt.Errorf("failed to parse json rpc address. Error: %w", err)
	}

	return sidechainHelper.ValidateSecretFlags(v.accountDir, v.accountConfig)
}

type vestingStatusResult struct {
	ValidatorAddress        string `json:"validatorAddress"`
	VestingDuration         string `json:"vestingDuration"`
	VestingStart            string `json:"vestingStart"`
	VestingEnd              string `json:"vestingEnd"`
	BaseStake               string `json:"baseStake"`
	VestBonus               string `json:"vestBonus"`
	RSIBonus                string `json:"rsiBonus"`
	VestingCommission       string `json:"vestingCommission"`
	GeneratedRewards        string `json:"generatedRewards"`
	ClaimableRewards        string `json:"claimableRewards"`
	ClaimableCommissions    string `json:"claimableCommissions"`
	IsActiveVestingPosition bool   `json:"isActiveVestingPosition"`
}

func formatTimestamp(ts *big.Int) string {
	if ts == nil || ts.Sign() == 0 {
		return "N/A"
	}

	t := time.Unix(ts.Int64(), 0).UTC()

	return fmt.Sprintf("%s (%s)", ts.String(), t.Format(time.RFC3339))
}

func formatWei(wei *big.Int) string {
	if wei == nil {
		return "0"
	}

	return wei.String()
}

func (vr vestingStatusResult) GetOutput() string {
	var buffer bytes.Buffer

	if !vr.IsActiveVestingPosition {
		buffer.WriteString("\n[VESTING STATUS - NO ACTIVE POSITION]\n")

		vals := []string{
			fmt.Sprintf("Validator Address|%s", vr.ValidatorAddress),
			fmt.Sprintf("Claimable Rewards (wei)|%s", vr.ClaimableRewards),
			fmt.Sprintf("Claimable Commissions (wei)|%s", vr.ClaimableCommissions),
		}

		buffer.WriteString(helper.FormatKV(vals))
		buffer.WriteString("\n")

		return buffer.String()
	}

	buffer.WriteString("\n[VESTING STATUS]\n")

	vals := []string{
		fmt.Sprintf("Validator Address|%s", vr.ValidatorAddress),
		fmt.Sprintf("Vesting Duration (weeks)|%s", vr.VestingDuration),
		fmt.Sprintf("Vesting Start|%s", vr.VestingStart),
		fmt.Sprintf("Vesting End|%s", vr.VestingEnd),
		fmt.Sprintf("Base Stake (wei)|%s", vr.BaseStake),
		fmt.Sprintf("Vest Bonus (wei)|%s", vr.VestBonus),
		fmt.Sprintf("RSI Bonus (wei)|%s", vr.RSIBonus),
		fmt.Sprintf("Vesting Commission|%s", vr.VestingCommission),
		fmt.Sprintf("Generated Rewards (wei)|%s", vr.GeneratedRewards),
		fmt.Sprintf("Claimable Rewards (wei)|%s", vr.ClaimableRewards),
		fmt.Sprintf("Claimable Commissions (wei)|%s", vr.ClaimableCommissions),
	}

	buffer.WriteString(helper.FormatKV(vals))
	buffer.WriteString("\n")

	return buffer.String()
}
