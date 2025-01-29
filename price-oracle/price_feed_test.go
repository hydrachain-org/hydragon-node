package priceoracle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildCoingeckoReq(t *testing.T) {
	origTimeNow := timeNow
	defer func() {
		timeNow = origTimeNow
	}()

	// Fix the current time to 2025-01-23 10:00 UTC
	fixedTime := time.Date(2025, 1, 23, 10, 0, 0, 0, time.UTC)
	timeNow = func() time.Time {
		return fixedTime
	}

	// Table of test cases
	testCases := []struct {
		name               string
		isPriceOracleFixed bool
		expectedDate       string // we expect this to appear in the URL
	}{
		{
			name:               "Fork not fixed => uses yesterday",
			isPriceOracleFixed: false,
			expectedDate:       "22-01-2025", // one day before fixedTime
		},
		{
			name:               "Fork fixed => uses today",
			isPriceOracleFixed: true,
			expectedDate:       "23-01-2025", // same as fixedTime
		},
	}

	priceFeed := &priceFeed{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := priceFeed.buildCoingeckoReq(tc.isPriceOracleFixed)
			require.NoError(t, err, "building request should not fail")

			url := req.URL.String()
			require.Containsf(t, url, tc.expectedDate,
				"URL %q does not contain expected date %q", url, tc.expectedDate)
		})
	}
}
