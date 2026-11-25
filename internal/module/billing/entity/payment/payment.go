// Package payment holds the billing domain's payment method entity and the
// stored configuration of each gateway it can be bound to.
package payment

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

type Payment struct {
	Id          int64  `gorm:"primaryKey"`
	Name        string `gorm:"type:varchar(100);not null;default:'';comment:Payment Name"`
	Platform    string `gorm:"<-:create;type:varchar(100);not null;comment:Payment Platform"`
	Icon        string `gorm:"type:varchar(255);default:'';comment:Payment Icon"`
	Domain      string `gorm:"type:varchar(255);default:'';comment:Notification Domain"`
	Config      string `gorm:"type:text;not null;comment:Payment Configuration"`
	Description string `gorm:"type:text;comment:Payment Description"`
	FeeMode     uint   `gorm:"type:tinyint(1);not null;default:0;comment:Fee Mode: 0: No Fee 1: Percentage 2: Fixed Amount 3: Percentage + Fixed Amount"`
	FeePercent  int64  `gorm:"type:int;default:0;comment:Fee Percentage"`
	FeeAmount   int64  `gorm:"type:int;default:0;comment:Fixed Fee Amount"`
	Sort        int64  `gorm:"type:int;not null;default:0;comment:Sort"`
	Enable      *bool  `gorm:"type:tinyint(1);not null;default:0;comment:Is Enabled"`
	Token       string `gorm:"type:varchar(255);unique;not null;default:'';comment:Payment Token"`
}

func (*Payment) TableName() string {
	return "payment"
}

func (l *Payment) BeforeCreate(tx *gorm.DB) error {
	if l.Sort == 0 {
		var maxSort int64
		if err := tx.Model(&Payment{}).Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return err
		}
		l.Sort = maxSort + 1
	}
	return nil
}

func (l *Payment) BeforeDelete(_ *gorm.DB) (err error) {
	if l.Id == -1 {
		return fmt.Errorf("can't delete default payment method")
	}
	return nil
}

type Filter struct {
	Mark   string
	Enable *bool
	Search string
}

type StripeConfig struct {
	PublicKey     string `json:"public_key"`
	SecretKey     string `json:"secret_key"`
	WebhookSecret string `json:"webhook_secret"`
	// WebhookEndpointID is the Stripe webhook endpoint registered for the
	// method, so it can be removed when the secret key is rotated to another
	// account; empty on methods registered before it was recorded.
	WebhookEndpointID string `json:"webhook_endpoint_id,omitempty"`
	Payment           string `json:"payment"`
}

func (l *StripeConfig) Marshal() ([]byte, error) {
	type Alias StripeConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *StripeConfig) Unmarshal(data []byte) error {
	type Alias StripeConfig
	aux := (*Alias)(l)
	return json.Unmarshal(data, &aux)
}

type AlipayF2FConfig struct {
	AppId       string `json:"app_id"`
	PrivateKey  string `json:"private_key"`
	PublicKey   string `json:"public_key"`
	InvoiceName string `json:"invoice_name"`
	Sandbox     bool   `json:"sandbox"`
	// Gateway optionally overrides the sandbox gateway URL (Alipay has
	// retired sandbox hosts before); production traffic ignores it.
	Gateway string `json:"gateway,omitempty"`
}

func (l *AlipayF2FConfig) Marshal() ([]byte, error) {
	type Alias AlipayF2FConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *AlipayF2FConfig) Unmarshal(data []byte) error {
	// First try to unmarshal into a map to handle string "true"/"false" for sandbox
	var rawMap map[string]any
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	// Convert sandbox field if it's a string
	if sandboxVal, ok := rawMap["sandbox"]; ok {
		switch v := sandboxVal.(type) {
		case string:
			rawMap["sandbox"] = v == "true" || v == "1"
		case bool:
			// Already a bool, no conversion needed
		}
	}

	// Re-marshal and unmarshal into the struct
	convertedData, err := json.Marshal(rawMap)
	if err != nil {
		return err
	}
	type Alias AlipayF2FConfig
	return json.Unmarshal(convertedData, (*Alias)(l))
}

type CryptomusConfig struct {
	MerchantID string `json:"merchant_id"`
	APIKey     string `json:"api_key"`
}

func (l *CryptomusConfig) Marshal() ([]byte, error) {
	type Alias CryptomusConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *CryptomusConfig) Unmarshal(data []byte) error {
	type Alias CryptomusConfig
	aux := (*Alias)(l)
	return json.Unmarshal(data, &aux)
}

type EPayConfig struct {
	Pid  string `json:"pid"`
	Url  string `json:"url"`
	Key  string `json:"key"`
	Type string `json:"type"`
	// Empty Version and CheckoutMode retain the existing V1 page checkout.
	Version      string `json:"version,omitempty"`
	CheckoutMode string `json:"checkout_mode,omitempty"`
	PrivateKey   string `json:"private_key,omitempty"`
	PublicKey    string `json:"public_key,omitempty"`
}

func (l *EPayConfig) Marshal() ([]byte, error) {
	type Alias EPayConfig
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

func (l *EPayConfig) Unmarshal(data []byte) error {
	type Alias EPayConfig
	aux := (*Alias)(l)
	return json.Unmarshal(data, &aux)
}

// NanoConfig is a Nano (XNO) payment method. PayTo receives the payments,
// NodeURL is the node RPC the payments are read from, and XNOPerUnit is the
// operator's price in XNO of one unit of Currency, the currency the method
// charges. It holds no key and no seed.
type NanoConfig struct {
	PayTo      string `json:"pay_to"`
	NodeURL    string `json:"node_url"`
	Currency   string `json:"currency"`
	XNOPerUnit string `json:"xno_per_unit"`
}

func (l *NanoConfig) Marshal() ([]byte, error) {
	type Alias NanoConfig
	return json.Marshal((*Alias)(l))
}

func (l *NanoConfig) Unmarshal(data []byte) error {
	type Alias NanoConfig
	return json.Unmarshal(data, (*Alias)(l))
}
