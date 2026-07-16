package dto

type AlipayNotifyResponse struct {
	ReturnCode string `json:"return_code"`
}

type CreatePaymentMethodRequest struct {
	Name        string      `json:"name" validate:"required"`
	Platform    string      `json:"platform" validate:"required"`
	Description string      `json:"description"`
	Icon        string      `json:"icon,omitempty"`
	Domain      string      `json:"domain,omitempty"`
	Config      interface{} `json:"config" validate:"required"`
	FeeMode     uint        `json:"fee_mode"`
	FeePercent  int64       `json:"fee_percent,omitempty"`
	FeeAmount   int64       `json:"fee_amount,omitempty"`
	Sort        int64       `json:"sort,omitempty"`
	Enable      *bool       `json:"enable" validate:"required"`
}

type DeletePaymentMethodRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type EPayNotifyRequest struct {
	Pid         string `json:"pid"          form:"pid"`
	TradeNo     string `json:"trade_no"     form:"trade_no"`
	OutTradeNo  string `json:"out_trade_no" form:"out_trade_no"`
	Type        string `json:"type"         form:"type"`
	Name        string `json:"name"         form:"name"`
	Money       string `json:"money"        form:"money"`
	TradeStatus string `json:"trade_status" form:"trade_status"`
	Param       string `json:"param"        form:"param"`
	Sign        string `json:"sign"         form:"sign"`
	SignType    string `json:"sign_type"    form:"sign_type"`
}

type GetAvailablePaymentMethodsResponse struct {
	List []PaymentMethod `json:"list"`
}

type GetPaymentMethodListRequest struct {
	Page     int    `form:"page" validate:"required,gt=0"`
	Size     int    `form:"size" validate:"required,gt=0,lte=100"`
	Platform string `form:"platform,omitempty"`
	Search   string `form:"search,omitempty"`
	Enable   *bool  `form:"enable,omitempty"`
}

type GetPaymentMethodListResponse struct {
	Total int64                 `json:"total"`
	List  []PaymentMethodDetail `json:"list"`
}

type PaymentConfig struct {
	Id          int64       `json:"id" validate:"required"`
	Name        string      `json:"name" validate:"required"`
	Platform    string      `json:"platform" validate:"required"`
	Description string      `json:"description"`
	Icon        string      `json:"icon,omitempty"`
	Domain      string      `json:"domain,omitempty"`
	Config      interface{} `json:"config" validate:"required"`
	FeeMode     uint        `json:"fee_mode"`
	FeePercent  int64       `json:"fee_percent,omitempty"`
	FeeAmount   int64       `json:"fee_amount,omitempty"`
	Sort        int64       `json:"sort,omitempty"`
	Enable      *bool       `json:"enable" validate:"required"`
}

type PaymentMethod struct {
	Id          int64  `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	FeeMode     uint   `json:"fee_mode"`
	FeePercent  int64  `json:"fee_percent"`
	FeeAmount   int64  `json:"fee_amount"`
	Sort        int64  `json:"sort"`
}

type PaymentMethodDetail struct {
	Id          int64       `json:"id"`
	Name        string      `json:"name"`
	Platform    string      `json:"platform"`
	Description string      `json:"description"`
	Icon        string      `json:"icon"`
	Domain      string      `json:"domain"`
	Config      interface{} `json:"config"`
	FeeMode     uint        `json:"fee_mode"`
	FeePercent  int64       `json:"fee_percent"`
	FeeAmount   int64       `json:"fee_amount"`
	Sort        int64       `json:"sort"`
	Enable      bool        `json:"enable"`
	NotifyURL   string      `json:"notify_url"`
}

type PlatformInfo struct {
	Platform                 string            `json:"platform"`
	PlatformUrl              string            `json:"platform_url"`
	PlatformFieldDescription map[string]string `json:"platform_field_description"`
}

type PlatformResponse struct {
	List []PlatformInfo `json:"list"`
}

type StripePayment struct {
	Method         string `json:"method"`
	ClientSecret   string `json:"client_secret"`
	PublishableKey string `json:"publishable_key"`
}

type UpdatePaymentMethodRequest struct {
	Id          int64       `json:"id" validate:"required"`
	Name        string      `json:"name" validate:"required"`
	Platform    string      `json:"platform" validate:"required"`
	Description string      `json:"description"`
	Icon        string      `json:"icon,omitempty"`
	Domain      string      `json:"domain,omitempty"`
	Config      interface{} `json:"config" validate:"required"`
	FeeMode     uint        `json:"fee_mode"`
	FeePercent  int64       `json:"fee_percent,omitempty"`
	FeeAmount   int64       `json:"fee_amount,omitempty"`
	Sort        int64       `json:"sort,omitempty"`
	Enable      *bool       `json:"enable" validate:"required"`
}
