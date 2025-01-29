package priceoracle

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/secrets"
	"github.com/0xPolygon/polygon-edge/types"
)

type PriceFeed interface {
	// GetPrice returns the USD price per 1 HYDRA with 8 decimals precision
	GetPrice(header *types.Header) (*big.Int, error)
}

type dummyPriceFeed struct{}

func NewDummyPriceFeed() (PriceFeed, error) {
	return &dummyPriceFeed{}, nil
}

func (d *dummyPriceFeed) GetPrice(header *types.Header) (*big.Int, error) {
	return nil, nil
}

type priceFeed struct {
	coinGeckoAPIKey string
	forks           *chain.Forks
}

func NewPriceFeed(secretsManagerConfig *secrets.SecretsManagerConfig, forks *chain.Forks) (PriceFeed, error) {
	apiKey, ok := secretsManagerConfig.Extra[secrets.CoinGeckoAPIKey].(string)
	if !ok {
		return nil, fmt.Errorf(secrets.CoinGeckoAPIKey + " is not a string")
	}

	return &priceFeed{coinGeckoAPIKey: apiKey, forks: forks}, nil
}

func (p *priceFeed) GetPrice(header *types.Header) (*big.Int, error) {
	forkConfig := p.forks.At(header.Number)

	req, err := p.buildCoingeckoReq(forkConfig.PriceOracleFix)
	if err != nil {
		return nil, fmt.Errorf("generating CoinGecko request failed: %w", err)
	}

	body, err := common.FetchData(req)
	if err != nil {
		return nil, fmt.Errorf("get price from CoinGecko failed: %w", err)
	}

	var priceData PriceDataCoinGecko

	err = json.Unmarshal(body, &priceData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	price, err := common.ConvertFloatToBigInt(priceData.MarketData.CurrentPrice.USD, 8)
	if err != nil {
		return nil, fmt.Errorf("failed to convert price to big.Int: %w", err)
	}

	return price, nil
}

// buildCoingeckoReq creates an HTTP request using either today or yesterday’s date, depending on isPriceOracleFixed.
func (p *priceFeed) buildCoingeckoReq(isPriceOracleFixed bool) (*http.Request, error) {
	var date string
	if isPriceOracleFixed {
		date = getDateFormatted(timeNow().UTC())
	} else {
		date = getDateFormatted(timeNow().UTC().AddDate(0, 0, -1))
	}

	apiURL := fmt.Sprintf(`https://api.coingecko.com/api/v3/coins/hydra/history?date=%s`, date)

	req, err := common.GenerateThirdPartyJSONRequest(apiURL)
	if err != nil {
		return nil, err
	}

	// Add the key in the header
	req.Header.Add("x-cg-demo-api-key", p.coinGeckoAPIKey)

	return req, nil
}

type PriceDataCoinGecko struct {
	ID         string `json:"id"`
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	MarketData struct {
		CurrentPrice struct {
			USD float64 `json:"usd"`
		} `json:"current_price"`
	} `json:"market_data"`
}

// getDateFormatted returns the date in the format dd-mm-yyyy for a given time.
func getDateFormatted(time time.Time) string {
	return time.Format("02-01-2006")
}

var timeNow = func() time.Time {
	return time.Now().UTC()
}
