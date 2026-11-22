// Package exchangerate converts the site currency for gateways that collect
// another one: the apilayer conversion API and the cache of the refreshed CNY
// rate that checkout reads first.
package exchangerate

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/perfect-panel/backend/pkg/logger"
)

const (
	Url = "https://api.apilayer.com"
)

type Response struct {
	Success bool   `json:"success"`
	Terms   string `json:"terms"`
	Privacy string `json:"privacy"`
	Query   struct {
		From   string  `json:"from"`
		To     string  `json:"to"`
		Amount float64 `json:"amount"`
	} `json:"query"`
	Info struct {
		Timestamp int64   `json:"timestamp"`
		Quote     float64 `json:"quote"`
	} `json:"info"`
	Result float64 `json:"result"`
}

// GetExchangeRate converts amount units of form into to at the rate API.
func GetExchangeRate(form, to, access string, amount float64) (float64, error) {
	return convert(Url, form, to, access, amount)
}

func convert(baseURL, from, to, access string, amount float64) (float64, error) {
	client := resty.New()
	client.SetRetryCount(3)
	client.SetTimeout(5 * time.Second)
	client.SetBaseURL(baseURL)
	client.SetQueryParams(map[string]string{
		"from":   from,
		"to":     to,
		"amount": strconv.FormatFloat(amount, 'f', -1, 64),
	})
	result := new(Response)
	resp, err := client.R().SetHeader("apikey", access).SetResult(result).Get("/currency_data/convert")
	if err != nil {
		return 0, err
	}
	if !result.Success {
		logger.Info("Exchange Rate Response: ", resp.String())
		return 0, errors.New("exchange rate failed")
	}
	return result.Result, nil
}

// rateCache is the refreshed rate of the site currency in CNY.
type rateCache interface {
	Get() float64
	Set(float64)
}

// Source prices the site currency for gateways that collect CNY. The rate
// refresh task keeps Cache current; without a cached rate the rate API is
// asked with AccessKey and the answer is cached.
type Source struct {
	Cache     rateCache
	AccessKey string
	// BaseURL overrides the rate API; tests point it at a local fake.
	BaseURL string
}

// Rate is the price of one unit of from in units of to.
func (s Source) Rate(_ context.Context, from, to string) (float64, error) {
	toCNY := strings.EqualFold(to, "CNY")
	if toCNY && s.Cache != nil {
		if rate := s.Cache.Get(); rate != 0 {
			return rate, nil
		}
	}
	// A gateway must never be sent a value merely relabelled as another
	// currency: without a conversion source the payment is refused.
	if s.AccessKey == "" {
		return 0, errors.New("exchange rate is not configured")
	}
	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = Url
	}
	rate, err := convert(baseURL, from, to, s.AccessKey, 1)
	if err != nil {
		return 0, err
	}
	if toCNY && s.Cache != nil {
		s.Cache.Set(rate)
	}
	return rate, nil
}
