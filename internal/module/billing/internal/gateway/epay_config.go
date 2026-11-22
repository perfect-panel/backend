package gateway

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	paymentEntity "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/epay"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment/epay/protocol"
)

// Version is selected by stored configuration, never by untrusted callback data.
func newEPayGateway(config paymentEntity.EPayConfig, httpClient *http.Client) (*epayGateway, error) {
	config.Version = strings.ToLower(strings.TrimSpace(config.Version))
	config.CheckoutMode = strings.ToLower(strings.TrimSpace(config.CheckoutMode))
	config.Url = strings.TrimSpace(config.Url)
	if config.CheckoutMode != "" && config.CheckoutMode != "page" && config.CheckoutMode != "api" {
		return nil, errors.New("EPay checkout_mode must be page or api")
	}
	if config.CheckoutMode == "api" && config.Type == "" {
		return nil, errors.New("EPay API checkout requires a payment type")
	}
	g := &epayGateway{config: config}
	var err error
	switch config.Version {
	case "", "v1":
		if config.PrivateKey != "" || config.PublicKey != "" {
			return nil, errors.New("EPay RSA keys require version v2")
		}
		g.client = epay.NewClient(config.Pid, config.Url, config.Key, config.Type, epay.WithHTTPClient(httpClient))
		if err = g.client.Validate(); err != nil {
			return nil, err
		}
		if config.CheckoutMode == "api" {
			g.v1, err = protocol.NewV1Client(protocol.V1Config{
				BaseURL: config.Url, MerchantID: config.Pid, Key: config.Key, HTTPClient: httpClient,
			})
		}
	case "v2":
		if config.Key != "" {
			return nil, errors.New("EPay V2 uses RSA keys; remove the V1 shared key")
		}
		switch config.Type {
		case "", "alipay", "wxpay", "qqpay":
		default:
			return nil, errors.New("unsupported EPay V2 payment type")
		}
		g.v2, err = protocol.NewClient(protocol.Config{
			BaseURL: config.Url, MerchantID: config.Pid,
			MerchantPrivateKey: []byte(config.PrivateKey), PlatformPublicKey: []byte(config.PublicKey), HTTPClient: httpClient,
		})
	default:
		return nil, errors.New("EPay version must be v1 or v2")
	}
	if err != nil {
		return nil, err
	}
	return g, nil
}

func singleValues(params map[string]string) url.Values {
	values := make(url.Values, len(params))
	for key, value := range params {
		values.Set(key, value)
	}
	return values
}
