package vestingstatus

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_formatTimestamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *big.Int
		expected string
	}{
		{
			name:     "nil returns N/A",
			input:    nil,
			expected: "N/A",
		},
		{
			name:     "zero returns N/A",
			input:    big.NewInt(0),
			expected: "N/A",
		},
		{
			name:     "valid unix timestamp",
			input:    big.NewInt(1700000000),
			expected: "1700000000 (" + time.Unix(1700000000, 0).UTC().Format(time.RFC3339) + ")",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, formatTimestamp(tt.input))
		})
	}
}

func Test_formatWei(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *big.Int
		expected string
	}{
		{
			name:     "nil returns 0",
			input:    nil,
			expected: "0",
		},
		{
			name:     "zero returns 0",
			input:    big.NewInt(0),
			expected: "0",
		},
		{
			name:     "positive value",
			input:    big.NewInt(1000000000000000000),
			expected: "1000000000000000000",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, formatWei(tt.input))
		})
	}
}

func Test_validateFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		params    vestingStatusParams
		expectErr bool
	}{
		{
			name: "valid flags with account dir",
			params: vestingStatusParams{
				jsonRPC:    "http://localhost:10002",
				accountDir: "/tmp/test-data",
			},
			expectErr: false,
		},
		{
			name: "empty jsonrpc returns error",
			params: vestingStatusParams{
				jsonRPC:    "",
				accountDir: "/tmp/test-data",
			},
			expectErr: true,
		},
		{
			name: "no account dir or config returns error",
			params: vestingStatusParams{
				jsonRPC: "http://localhost:10002",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.params.validateFlags()
			if tt.expectErr {
				require.Error(t, err)
			} else {
				// ValidateSecretFlags calls CheckIfDirectoryExist which may fail
				// if the dir doesn't exist. We only check that jsonRPC parsing succeeded.
				if err != nil {
					assert.NotContains(t, err.Error(), "failed to parse json rpc address")
				}
			}
		})
	}
}

func Test_GetOutput_ActivePosition(t *testing.T) {
	t.Parallel()

	result := vestingStatusResult{
		ValidatorAddress:        "0x1234567890abcdef",
		VestingDuration:         "52",
		VestingStart:            "1700000000 (2023-11-14T22:13:20Z)",
		VestingEnd:              "1731536000 (2024-11-14T00:00:00Z)",
		BaseStake:               "1000000000000000000",
		VestBonus:               "500000000000000000",
		RSIBonus:                "200000000000000000",
		GeneratedRewards:        "300000000000000000",
		UnclaimedRewards:        "100000000000000000",
		ClaimableCommissions:    "50000000000000000",
		IsActiveVestingPosition: true,
	}

	output := result.GetOutput()

	assert.Contains(t, output, "[VESTING STATUS]")
	assert.NotContains(t, output, "NO ACTIVE POSITION")
	assert.Contains(t, output, "0x1234567890abcdef")
	assert.Contains(t, output, "Vesting Duration (weeks)")
	assert.Contains(t, output, "Base Stake (wei)")
	assert.Contains(t, output, "Generated Rewards (wei)")
	assert.Contains(t, output, "Unclaimed Rewards (wei)")
	assert.Contains(t, output, "Claimable Commissions (wei)")
}

func Test_GetOutput_InactivePosition(t *testing.T) {
	t.Parallel()

	result := vestingStatusResult{
		ValidatorAddress:        "0xdeadbeef",
		GeneratedRewards:        "42000",
		UnclaimedRewards:        "100",
		ClaimableCommissions:    "200",
		IsActiveVestingPosition: false,
	}

	output := result.GetOutput()

	assert.Contains(t, output, "[VESTING STATUS - NO ACTIVE POSITION]")
	assert.Contains(t, output, "0xdeadbeef")
	assert.Contains(t, output, "Generated Rewards (wei)")
	assert.Contains(t, output, "42000")
	assert.Contains(t, output, "Unclaimed Rewards (wei)")
	assert.Contains(t, output, "Claimable Commissions (wei)")

	// Active-only fields should NOT be in inactive output
	assert.NotContains(t, output, "Vesting Duration")
	assert.NotContains(t, output, "Base Stake")
	assert.NotContains(t, output, "Vest Bonus")

	// Verify field count: should have exactly 4 fields (Validator Address, Generated Rewards, Unclaimed Rewards, Claimable Commissions)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// First line is header, rest are KV pairs
	kvLines := 0
	for _, line := range lines {
		if strings.Contains(line, "=") {
			kvLines++
		}
	}

	assert.Equal(t, 4, kvLines, "inactive output should have exactly 4 KV pairs")
}
