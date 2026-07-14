package types

type Ads struct {
	Id          int    `json:"id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	Status      int    `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type AlipayNotifyResponse struct {
	ReturnCode string `json:"return_code"`
}

type Announcement struct {
	Id        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Show      *bool  `json:"show"`
	Pinned    *bool  `json:"pinned"`
	Popup     *bool  `json:"popup"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type AnyTLS struct {
	Port           int            `json:"port" validate:"required"`
	SecurityConfig SecurityConfig `json:"security_config"`
}

type AppUserSubcbribe struct {
	Id          int64                   `json:"id"`
	Name        string                  `json:"name"`
	Upload      int64                   `json:"upload"`
	Traffic     int64                   `json:"traffic"`
	Download    int64                   `json:"download"`
	DeviceLimit int64                   `json:"device_limit"`
	StartTime   string                  `json:"start_time"`
	ExpireTime  string                  `json:"expire_time"`
	List        []AppUserSubscbribeNode `json:"list"`
}

type AppUserSubscbribeNode struct {
	Id           int64    `json:"id"`
	Name         string   `json:"name"`
	Uuid         string   `json:"uuid"`
	Protocol     string   `json:"protocol"`
	RelayMode    string   `json:"relay_mode"`
	RelayNode    string   `json:"relay_node"`
	ServerAddr   string   `json:"server_addr"`
	SpeedLimit   int      `json:"speed_limit"`
	Tags         []string `json:"tags"`
	Traffic      int64    `json:"traffic"`
	TrafficRatio float64  `json:"traffic_ratio"`
	Upload       int64    `json:"upload"`
	Config       string   `json:"config"`
	Country      string   `json:"country"`
	City         string   `json:"city"`
	Latitude     string   `json:"latitude"`
	Longitude    string   `json:"longitude"`
	CreatedAt    int64    `json:"created_at"`
	Download     int64    `json:"download"`
}

type AppleLoginCallbackRequest struct {
	Code    string `form:"code"`
	IDToken string `form:"id_token"`
	State   string `form:"state"`
}

type Application struct {
	Id            int64  `json:"id"`
	Icon          string `json:"icon"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	SubscribeType string `json:"subscribe_type"`
}

type ApplicationPlatform struct {
	IOS     []*ApplicationVersion `json:"ios,omitempty"`
	MacOS   []*ApplicationVersion `json:"macos,omitempty"`
	Linux   []*ApplicationVersion `json:"linux,omitempty"`
	Android []*ApplicationVersion `json:"android,omitempty"`
	Windows []*ApplicationVersion `json:"windows,omitempty"`
	Harmony []*ApplicationVersion `json:"harmony,omitempty"`
}

type ApplicationResponse struct {
	Applications []ApplicationResponseInfo `json:"applications"`
}

type ApplicationResponseInfo struct {
	Id            int64               `json:"id"`
	Name          string              `json:"name"`
	Icon          string              `json:"icon"`
	Description   string              `json:"description"`
	SubscribeType string              `json:"subscribe_type"`
	Platform      ApplicationPlatform `json:"platform"`
}

type ApplicationVersion struct {
	Id          int64  `json:"id"`
	Url         string `json:"url"`
	Version     string `json:"version" validate:"required"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
}

type AuthConfig struct {
	Mobile   MobileAuthenticateConfig `json:"mobile"`
	Email    EmailAuthticateConfig    `json:"email"`
	Device   DeviceAuthticateConfig   `json:"device"`
	Register PubilcRegisterConfig     `json:"register"`
}

type AuthMethodConfig struct {
	Id      int64       `json:"id"`
	Method  string      `json:"method"`
	Config  interface{} `json:"config"`
	Enabled bool        `json:"enabled"`
}

type BalanceLog struct {
	Type      uint16 `json:"type"`
	UserId    int64  `json:"user_id"`
	Amount    int64  `json:"amount"`
	OrderNo   string `json:"order_no,omitempty"`
	Balance   int64  `json:"balance"`
	Timestamp int64  `json:"timestamp"`
}

type BatchDeleteCouponRequest struct {
	Ids []int64 `json:"ids" validate:"required"`
}

type BatchDeleteDocumentRequest struct {
	Ids []int64 `json:"ids" validate:"required"`
}

type BatchDeleteSubscribeGroupRequest struct {
	Ids []int64 `json:"ids" validate:"required"`
}

type BatchDeleteSubscribeRequest struct {
	Ids []int64 `json:"ids" validate:"required"`
}

type BatchDeleteUserRequest struct {
	Ids []int64 `json:"ids" validate:"required"`
}

type BatchSendEmailTask struct {
	Id                int64  `json:"id"`
	Subject           string `json:"subject"`
	Content           string `json:"content"`
	Recipients        string `json:"recipients"`
	Scope             int8   `json:"scope"`
	RegisterStartTime int64  `json:"register_start_time"`
	RegisterEndTime   int64  `json:"register_end_time"`
	Additional        string `json:"additional"`
	Scheduled         int64  `json:"scheduled"`
	Interval          uint8  `json:"interval"`
	Limit             uint64 `json:"limit"`
	Status            uint8  `json:"status"`
	Errors            string `json:"errors"`
	Total             uint64 `json:"total"`
	Current           uint64 `json:"current"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

type BindOAuthCallbackRequest struct {
	Method   string      `json:"method"`
	Callback interface{} `json:"callback"`
}

type BindOAuthRequest struct {
	Method   string `json:"method"`
	Redirect string `json:"redirect"`
}

type BindOAuthResponse struct {
	Redirect string `json:"redirect"`
}

type BindTelegramResponse struct {
	Url       string `json:"url"`
	ExpiredAt int64  `json:"expired_at"`
}

type CheckUserRequest struct {
	Email string `form:"email" validate:"required"`
}

type CheckUserResponse struct {
	Exist bool `json:"exist"`
}

type CheckVerificationCodeRequest struct {
	Method  string `json:"method" validate:"required,oneof=email mobile"`
	Account string `json:"account" validate:"required"`
	Code    string `json:"code" validate:"required"`
	Type    uint8  `json:"type" validate:"required"`
}

type CheckVerificationCodeRespone struct {
	Status bool `json:"status"`
}

type CheckoutOrderRequest struct {
	OrderNo   string `json:"orderNo"`
	ReturnUrl string `json:"returnUrl,omitempty"`
}

type CheckoutOrderResponse struct {
	Type        string         `json:"type"`
	CheckoutUrl string         `json:"checkout_url,omitempty"`
	Stripe      *StripePayment `json:"stripe,omitempty"`
}

type CloseOrderRequest struct {
	OrderNo string `json:"orderNo" validate:"required"`
}

type CommissionLog struct {
	Type      uint16 `json:"type"`
	UserId    int64  `json:"user_id"`
	Amount    int64  `json:"amount"`
	OrderNo   string `json:"order_no"`
	Timestamp int64  `json:"timestamp"`
}

type CommissionWithdrawRequest struct {
	Amount  int64  `json:"amount"`
	Content string `json:"content"`
}

type Coupon struct {
	Id         int64   `json:"id"`
	Name       string  `json:"name"`
	Code       string  `json:"code"`
	Count      int64   `json:"count"`
	Type       uint8   `json:"type"`
	Discount   int64   `json:"discount"`
	StartTime  int64   `json:"start_time"`
	ExpireTime int64   `json:"expire_time"`
	UserLimit  int64   `json:"user_limit"`
	Subscribe  []int64 `json:"subscribe"`
	UsedCount  int64   `json:"used_count"`
	Enable     bool    `json:"enable"`
	CreatedAt  int64   `json:"created_at"`
	UpdatedAt  int64   `json:"updated_at"`
}

type CreateAdsRequest struct {
	Title       string `json:"title"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	Status      int    `json:"status"`
}

type CreateAnnouncementRequest struct {
	Title   string `json:"title" validate:"required"`
	Content string `json:"content" validate:"required"`
}

type CreateBatchSendEmailTaskRequest struct {
	Subject           string `json:"subject"`
	Content           string `json:"content"`
	Scope             int8   `json:"scope"`
	RegisterStartTime int64  `json:"register_start_time,omitempty"`
	RegisterEndTime   int64  `json:"register_end_time,omitempty"`
	Additional        string `json:"additional,omitempty"`
	Scheduled         int64  `json:"scheduled,omitempty"`
	Interval          uint8  `json:"interval,omitempty"`
	Limit             uint64 `json:"limit,omitempty"`
}

type CreateCouponRequest struct {
	Name       string  `json:"name" validate:"required"`
	Code       string  `json:"code,omitempty"`
	Count      int64   `json:"count,omitempty"`
	Type       uint8   `json:"type" validate:"required"`
	Discount   int64   `json:"discount" validate:"required"`
	StartTime  int64   `json:"start_time" validate:"required"`
	ExpireTime int64   `json:"expire_time" validate:"required"`
	UserLimit  int64   `json:"user_limit,omitempty"`
	Subscribe  []int64 `json:"subscribe,omitempty"`
	UsedCount  int64   `json:"used_count,omitempty"`
	Enable     *bool   `json:"enable,omitempty"`
}

type CreateDocumentRequest struct {
	Title   string   `json:"title" validate:"required"`
	Content string   `json:"content" validate:"required"`
	Tags    []string `json:"tags,omitempty" `
	Show    *bool    `json:"show"`
}

type CreateNodeRequest struct {
	Name     string   `json:"name"`
	Tags     []string `json:"tags,omitempty"`
	Port     uint16   `json:"port"`
	Address  string   `json:"address"`
	ServerId int64    `json:"server_id"`
	Protocol string   `json:"protocol"`
	Enabled  *bool    `json:"enabled"`
}

type CreateOrderRequest struct {
	UserId         int64  `json:"user_id" validate:"required"`
	Type           uint8  `json:"type" validate:"required"`
	Quantity       int64  `json:"quantity,omitempty" validate:"omitempty,lte=1000"`
	Price          int64  `json:"price" validate:"required,gte=0,lte=2000000000"`
	Amount         int64  `json:"amount" validate:"required,gte=0,lte=2147483647"`
	Discount       int64  `json:"discount,omitempty" validate:"omitempty,gte=0,lte=2000000000"`
	Coupon         string `json:"coupon,omitempty"`
	CouponDiscount int64  `json:"coupon_discount,omitempty" validate:"omitempty,gte=0,lte=2000000000"`
	Commission     int64  `json:"commission" validate:"gte=0,lte=2000000000"`
	FeeAmount      int64  `json:"fee_amount" validate:"required,gte=0,lte=2000000000"`
	PaymentId      int64  `json:"payment_id" validate:"required"`
	TradeNo        string `json:"trade_no,omitempty"`
	Status         uint8  `json:"status,omitempty"`
	SubscribeId    int64  `json:"subscribe_id,omitempty"`
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

type CreateQuotaTaskRequest struct {
	Subscribers  []int64 `json:"subscribers"`
	IsActive     *bool   `json:"is_active"`
	StartTime    int64   `json:"start_time"`
	EndTime      int64   `json:"end_time"`
	ResetTraffic bool    `json:"reset_traffic"`
	Days         uint64  `json:"days"`
	GiftType     uint8   `json:"gift_type"`
	GiftValue    uint64  `json:"gift_value"`
}

type CreateServerRequest struct {
	Name      string     `json:"name"`
	Country   string     `json:"country,omitempty"`
	City      string     `json:"city,omitempty"`
	Address   string     `json:"address"`
	Sort      int        `json:"sort,omitempty"`
	Protocols []Protocol `json:"protocols"`
}

type CreateSubscribeApplicationRequest struct {
	Name              string       `json:"name"`
	Description       string       `json:"description,omitempty"`
	Icon              string       `json:"icon,omitempty"`
	Scheme            string       `json:"scheme,omitempty"`
	UserAgent         string       `json:"user_agent"`
	IsDefault         bool         `json:"is_default"`
	SubscribeTemplate string       `json:"template"`
	OutputFormat      string       `json:"output_format"`
	DownloadLink      DownloadLink `json:"download_link"`
}

type CreateSubscribeGroupRequest struct {
	Name        string `json:"name" validate:"required"`
	Description string `json:"description"`
}

type CreateSubscribeRequest struct {
	Name              string              `json:"name" validate:"required"`
	Language          string              `json:"language"`
	Description       string              `json:"description"`
	UnitPrice         int64               `json:"unit_price"`
	UnitTime          string              `json:"unit_time"`
	Discount          []SubscribeDiscount `json:"discount"`
	Replacement       int64               `json:"replacement"`
	Inventory         int64               `json:"inventory"`
	Traffic           int64               `json:"traffic"`
	SpeedLimit        int64               `json:"speed_limit"`
	DeviceLimit       int64               `json:"device_limit"`
	Quota             int64               `json:"quota"`
	Nodes             StringInt64Slice    `json:"nodes"`
	NodeTags          []string            `json:"node_tags"`
	Show              *bool               `json:"show"`
	Sell              *bool               `json:"sell"`
	DeductionRatio    int64               `json:"deduction_ratio"`
	AllowDeduction    *bool               `json:"allow_deduction"`
	ResetCycle        int64               `json:"reset_cycle"`
	RenewalReset      *bool               `json:"renewal_reset"`
	ShowOriginalPrice bool                `json:"show_original_price"`
}

type CreateTicketFollowRequest struct {
	TicketId int64  `json:"ticket_id" validate:"required"`
	From     string `json:"from" validate:"required"`
	Type     uint8  `json:"type" validate:"required"`
	Content  string `json:"content" validate:"required"`
}

type CreateUserAuthMethodRequest struct {
	UserId         int64  `json:"user_id"`
	AuthType       string `json:"auth_type"`
	AuthIdentifier string `json:"auth_identifier"`
}

type CreateUserRequest struct {
	Email              string `json:"email"`
	Telephone          string `json:"telephone"`
	TelephoneAreaCode  string `json:"telephone_area_code"`
	Password           string `json:"password"`
	ProductId          int64  `json:"product_id"`
	Duration           int64  `json:"duration"`
	ReferralPercentage uint8  `json:"referral_percentage"`
	OnlyFirstPurchase  bool   `json:"only_first_purchase"`
	RefererUser        string `json:"referer_user"`
	ReferCode          string `json:"refer_code"`
	Balance            int64  `json:"balance"`
	Commission         int64  `json:"commission"`
	GiftAmount         int64  `json:"gift_amount"`
	IsAdmin            bool   `json:"is_admin"`
}

type CreateUserSubscribeRequest struct {
	UserId      int64 `json:"user_id"`
	ExpiredAt   int64 `json:"expired_at"`
	Traffic     int64 `json:"traffic"`
	SubscribeId int64 `json:"subscribe_id"`
}

type CreateUserTicketFollowRequest struct {
	TicketId int64  `json:"ticket_id"`
	From     string `json:"from"`
	Type     uint8  `json:"type"`
	Content  string `json:"content"`
}

type CreateUserTicketRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type Currency struct {
	CurrencyUnit   string `json:"currency_unit"`
	CurrencySymbol string `json:"currency_symbol"`
}

type CurrencyConfig struct {
	AccessKey      string `json:"access_key"`
	CurrencyUnit   string `json:"currency_unit"`
	CurrencySymbol string `json:"currency_symbol"`
}

type DeleteAdsRequest struct {
	Id int64 `json:"id"`
}

type DeleteAnnouncementRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type DeleteCouponRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type DeleteDocumentRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type DeleteNodeRequest struct {
	Id int64 `json:"id"`
}

type DeletePaymentMethodRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type DeleteServerRequest struct {
	Id int64 `json:"id"`
}

type DeleteSubscribeApplicationRequest struct {
	Id int64 `json:"id"`
}

type DeleteSubscribeGroupRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type DeleteSubscribeRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type DeleteUserAuthMethodRequest struct {
	UserId   int64  `json:"user_id"`
	AuthType string `json:"auth_type"`
}

type DeleteUserDeivceRequest struct {
	Id int64 `json:"id"`
}

type DeleteUserSubscribeRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id,string"`
}

type DeviceAuthticateConfig struct {
	Enable         bool `json:"enable"`
	ShowAds        bool `json:"show_ads"`
	EnableSecurity bool `json:"enable_security"`
	OnlyRealDevice bool `json:"only_real_device"`
}

type DeviceLoginRequest struct {
	Identifier string `json:"identifier" validate:"required"`
	IP         string `header:"X-Original-Forwarded-For"`
	UserAgent  string `json:"user_agent" validate:"required"`
	CfToken    string `json:"cf_token,optional"`
}

type Document struct {
	Id        int64    `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	Show      bool     `json:"show"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

type DownloadLink struct {
	IOS     string `json:"ios,omitempty"`
	Android string `json:"android,omitempty"`
	Windows string `json:"windows,omitempty"`
	Mac     string `json:"mac,omitempty"`
	Linux   string `json:"linux,omitempty"`
	Harmony string `json:"harmony,omitempty"`
}

type EPayNotifyRequest struct {
	Pid         int64  `json:"pid"          form:"pid"`
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

type EmailAuthticateConfig struct {
	Enable             bool   `json:"enable"`
	EnableVerify       bool   `json:"enable_verify"`
	EnableDomainSuffix bool   `json:"enable_domain_suffix"`
	DomainSuffixList   string `json:"domain_suffix_list"`
}

type FilterBalanceLogRequest struct {
	FilterLogParams
	UserId int64 `form:"user_id,optional"`
}

type FilterBalanceLogResponse struct {
	Total int64        `json:"total"`
	List  []BalanceLog `json:"list"`
}

type FilterCommissionLogRequest struct {
	FilterLogParams
	UserId int64 `form:"user_id,optional"`
}

type FilterCommissionLogResponse struct {
	Total int64           `json:"total"`
	List  []CommissionLog `json:"list"`
}

type FilterEmailLogResponse struct {
	Total int64        `json:"total"`
	List  []MessageLog `json:"list"`
}

type FilterGiftLogRequest struct {
	FilterLogParams
	UserId int64 `form:"user_id,optional"`
}

type FilterGiftLogResponse struct {
	Total int64     `json:"total"`
	List  []GiftLog `json:"list"`
}

type FilterLogParams struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Date   string `form:"date,optional"`
	Search string `form:"search,optional"`
}

type FilterLoginLogRequest struct {
	FilterLogParams
	UserId int64 `form:"user_id,optional"`
}

type FilterLoginLogResponse struct {
	Total int64      `json:"total"`
	List  []LoginLog `json:"list"`
}

type FilterMobileLogResponse struct {
	Total int64        `json:"total"`
	List  []MessageLog `json:"list"`
}

type FilterNodeListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Search string `form:"search,omitempty"`
}

type FilterNodeListResponse struct {
	Total int64  `json:"total"`
	List  []Node `json:"list"`
}

type FilterRegisterLogRequest struct {
	FilterLogParams
	UserId int64 `form:"user_id,optional"`
}

type FilterRegisterLogResponse struct {
	Total int64         `json:"total"`
	List  []RegisterLog `json:"list"`
}

type FilterResetSubscribeLogRequest struct {
	FilterLogParams
	UserSubscribeId int64 `form:"user_subscribe_id,optional"`
}

type FilterResetSubscribeLogResponse struct {
	Total int64               `json:"total"`
	List  []ResetSubscribeLog `json:"list"`
}

type FilterServerListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Search string `form:"search,omitempty"`
}

type FilterServerListResponse struct {
	Total int64    `json:"total"`
	List  []Server `json:"list"`
}

type FilterServerTrafficLogRequest struct {
	FilterLogParams
	ServerId int64 `form:"server_id,optional"`
}

type FilterServerTrafficLogResponse struct {
	Total int64              `json:"total"`
	List  []ServerTrafficLog `json:"list"`
}

type FilterSubscribeLogRequest struct {
	FilterLogParams
	UserId          int64 `form:"user_id,optional"`
	UserSubscribeId int64 `form:"user_subscribe_id,optional"`
}

type FilterSubscribeLogResponse struct {
	Total int64          `json:"total"`
	List  []SubscribeLog `json:"list"`
}

type FilterSubscribeTrafficRequest struct {
	FilterLogParams
	UserId          int64 `form:"user_id,optional"`
	UserSubscribeId int64 `form:"user_subscribe_id,optional"`
}

type FilterSubscribeTrafficResponse struct {
	Total int64                     `json:"total"`
	List  []UserSubscribeTrafficLog `json:"list"`
}

type FilterTrafficLogDetailsRequest struct {
	FilterLogParams
	ServerId    int64 `form:"server_id,optional"`
	SubscribeId int64 `form:"subscribe_id,optional"`
	UserId      int64 `form:"user_id,optional"`
}

type FilterTrafficLogDetailsResponse struct {
	Total int64               `json:"total"`
	List  []TrafficLogDetails `json:"list"`
}

type Follow struct {
	Id        int64  `json:"id"`
	TicketId  int64  `json:"ticket_id"`
	From      string `json:"from"`
	Type      uint8  `json:"type"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

type GetAdsDetailRequest struct {
	Id int64 `form:"id"`
}

type GetAdsListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Status *int   `form:"status,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetAdsListResponse struct {
	Total int64 `json:"total"`
	List  []Ads `json:"list"`
}

type GetAdsRequest struct {
	Device   string `form:"device"`
	Position string `form:"position"`
}

type GetAdsResponse struct {
	List []Ads `json:"list"`
}

type GetAnnouncementListRequest struct {
	Page   int64  `form:"page" validate:"required,gt=0"`
	Size   int64  `form:"size" validate:"required,gt=0,lte=100"`
	Show   *bool  `form:"show,omitempty"`
	Pinned *bool  `form:"pinned,omitempty"`
	Popup  *bool  `form:"popup,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetAnnouncementListResponse struct {
	Total int64          `json:"total"`
	List  []Announcement `json:"list"`
}

type GetAnnouncementRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type GetAuthMethodConfigRequest struct {
	Method string `form:"method"`
}

type GetAuthMethodListResponse struct {
	List []AuthMethodConfig `json:"list"`
}

type GetAvailablePaymentMethodsResponse struct {
	List []PaymentMethod `json:"list"`
}

type GetBatchSendEmailTaskListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Scope  *int8  `form:"scope,omitempty"`
	Status *uint8 `form:"status,omitempty"`
}

type GetBatchSendEmailTaskListResponse struct {
	Total int64                `json:"total"`
	List  []BatchSendEmailTask `json:"list"`
}

type GetBatchSendEmailTaskStatusRequest struct {
	Id int64 `json:"id"`
}

type GetBatchSendEmailTaskStatusResponse struct {
	Status  uint8  `json:"status"`
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Errors  string `json:"errors"`
}

type GetCouponListRequest struct {
	Page      int64  `form:"page" validate:"required,gt=0"`
	Size      int64  `form:"size" validate:"required,gt=0,lte=100"`
	Subscribe int64  `form:"subscribe,omitempty"`
	Search    string `form:"search,omitempty"`
}

type GetCouponListResponse struct {
	Total int64    `json:"total"`
	List  []Coupon `json:"list"`
}

type GetDetailRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type GetDeviceListResponse struct {
	List  []UserDevice `json:"list"`
	Total int64        `json:"total"`
}

type GetDocumentDetailRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type GetDocumentListRequest struct {
	Page   int64  `form:"page" validate:"required,gt=0"`
	Size   int64  `form:"size" validate:"required,gt=0,lte=100"`
	Tag    string `form:"tag,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetDocumentListResponse struct {
	Total int64      `json:"total"`
	List  []Document `json:"list"`
}

type GetGlobalConfigResponse struct {
	Site         SiteConfig             `json:"site"`
	Verify       VeifyConfig            `json:"verify"`
	Auth         AuthConfig             `json:"auth"`
	Invite       InviteConfig           `json:"invite"`
	Currency     Currency               `json:"currency"`
	Subscribe    SubscribeConfig        `json:"subscribe"`
	VerifyCode   PubilcVerifyCodeConfig `json:"verify_code"`
	OAuthMethods []string               `json:"oauth_methods"`
	WebAd        bool                   `json:"web_ad"`
}

type GetLoginLogRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type GetLoginLogResponse struct {
	List  []UserLoginLog `json:"list"`
	Total int64          `json:"total"`
}

type GetMessageLogListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Type   uint8  `form:"type"`
	Search string `form:"search,optional"`
}

type GetMessageLogListResponse struct {
	Total int64        `json:"total"`
	List  []MessageLog `json:"list"`
}

type GetNodeMultiplierResponse struct {
	Periods []TimePeriod `json:"periods"`
}

type GetOAuthMethodsResponse struct {
	Methods []UserAuthMethod `json:"methods"`
}

type GetOrderListRequest struct {
	Page        int64  `form:"page" validate:"required,gt=0"`
	Size        int64  `form:"size" validate:"required,gt=0,lte=100"`
	UserId      int64  `form:"user_id,omitempty"`
	Status      uint8  `form:"status,omitempty"`
	SubscribeId int64  `form:"subscribe_id,omitempty"`
	Search      string `form:"search,omitempty"`
}

type GetOrderListResponse struct {
	Total int64   `json:"total"`
	List  []Order `json:"list"`
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

type GetPreSendEmailCountRequest struct {
	Scope             int8  `json:"scope"`
	RegisterStartTime int64 `json:"register_start_time,omitempty"`
	RegisterEndTime   int64 `json:"register_end_time,omitempty"`
}

type GetPreSendEmailCountResponse struct {
	Count int64 `json:"count"`
}

type GetServerConfigRequest struct {
	ServerCommon
}

type GetServerConfigResponse struct {
	Basic    ServerBasic `json:"basic"`
	Protocol string      `json:"protocol"`
	Config   interface{} `json:"config"`
}

type GetServerProtocolsRequest struct {
	Id int64 `form:"id"`
}

type GetServerProtocolsResponse struct {
	Protocols []Protocol `json:"protocols"`
}

type GetServerUserListRequest struct {
	ServerCommon
}

type GetServerUserListResponse struct {
	Users []ServerUser `json:"users"`
}

type GetStatResponse struct {
	User     int64    `json:"user"`
	Node     int64    `json:"node"`
	Country  int64    `json:"country"`
	Protocol []string `json:"protocol"`
}

type GetSubscribeApplicationListRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type GetSubscribeApplicationListResponse struct {
	Total int64                  `json:"total"`
	List  []SubscribeApplication `json:"list"`
}

type GetSubscribeClientResponse struct {
	Total int64             `json:"total"`
	List  []SubscribeClient `json:"list"`
}

type GetSubscribeDetailsRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type GetSubscribeGroupListResponse struct {
	List  []SubscribeGroup `json:"list"`
	Total int64            `json:"total"`
}

type GetSubscribeListRequest struct {
	Page     int64  `form:"page" validate:"required,gt=0"`
	Size     int64  `form:"size" validate:"required,gt=0,lte=100"`
	Language string `form:"language,omitempty"`
	Search   string `form:"search,omitempty"`
}

type GetSubscribeListResponse struct {
	List  []SubscribeItem `json:"list"`
	Total int64           `json:"total"`
}

type GetSubscribeLogRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type GetSubscribeLogResponse struct {
	List  []UserSubscribeLog `json:"list"`
	Total int64              `json:"total"`
}

type GetSubscriptionRequest struct {
	Language string `form:"language"`
}

type GetSubscriptionResponse struct {
	List []Subscribe `json:"list"`
}

type GetTicketListRequest struct {
	Page   int64  `form:"page" validate:"required,gt=0"`
	Size   int64  `form:"size" validate:"required,gt=0,lte=100"`
	UserId int64  `form:"user_id,omitempty"`
	Status *uint8 `form:"status,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetTicketListResponse struct {
	Total int64    `json:"total"`
	List  []Ticket `json:"list"`
}

type GetTicketRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type GetTosResponse struct {
	TosContent string `json:"tos_content"`
}

type GetUserAuthMethodRequest struct {
	UserId int64 `json:"user_id"`
}

type GetUserAuthMethodResponse struct {
	AuthMethods []UserAuthMethod `json:"auth_methods"`
}

type GetUserListRequest struct {
	Page               int    `form:"page" validate:"required,gt=0"`
	Size               int    `form:"size" validate:"required,gt=0,lte=100"`
	Search             string `form:"search,omitempty"`
	UserId             *int64 `form:"user_id,omitempty"`
	Unscoped           bool   `form:"unscoped,omitempty"`
	SubscribeId        *int64 `form:"subscribe_id,omitempty"`
	UserSubscribeId    *int64 `form:"user_subscribe_id,omitempty"`
	UserSubscribeToken string `form:"user_subscribe_token,omitempty"`
}

type GetUserListResponse struct {
	Total int64  `json:"total"`
	List  []User `json:"list"`
}

type GetUserLoginLogsRequest struct {
	Page   int   `form:"page" validate:"required,gt=0"`
	Size   int   `form:"size" validate:"required,gt=0,lte=100"`
	UserId int64 `form:"user_id"`
}

type GetUserLoginLogsResponse struct {
	List  []UserLoginLog `json:"list"`
	Total int64          `json:"total"`
}

type GetUserSubscribeByIdRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type GetUserSubscribeDevicesRequest struct {
	Page        int   `form:"page" validate:"required,gt=0"`
	Size        int   `form:"size" validate:"required,gt=0,lte=100"`
	UserId      int64 `form:"user_id"`
	SubscribeId int64 `form:"subscribe_id"`
}

type GetUserSubscribeDevicesResponse struct {
	List  []UserDevice `json:"list"`
	Total int64        `json:"total"`
}

type GetUserSubscribeListRequest struct {
	Page   int   `form:"page" validate:"required,gt=0"`
	Size   int   `form:"size" validate:"required,gt=0,lte=100"`
	UserId int64 `form:"user_id"`
}

type GetUserSubscribeListResponse struct {
	List  []UserSubscribe `json:"list"`
	Total int64           `json:"total"`
}

type GetUserSubscribeLogsRequest struct {
	Page        int   `form:"page" validate:"required,gt=0"`
	Size        int   `form:"size" validate:"required,gt=0,lte=100"`
	UserId      int64 `form:"user_id"`
	SubscribeId int64 `form:"subscribe_id,omitempty"`
}

type GetUserSubscribeLogsResponse struct {
	List  []UserSubscribeLog `json:"list"`
	Total int64              `json:"total"`
}

type GetUserSubscribeResetTrafficLogsRequest struct {
	Page            int   `form:"page" validate:"required,gt=0"`
	Size            int   `form:"size" validate:"required,gt=0,lte=100"`
	UserSubscribeId int64 `form:"user_subscribe_id"`
}

type GetUserSubscribeResetTrafficLogsResponse struct {
	List  []ResetSubscribeTrafficLog `json:"list"`
	Total int64                      `json:"total"`
}

type GetUserSubscribeTrafficLogsRequest struct {
	Page        int   `form:"page" validate:"required,gt=0"`
	Size        int   `form:"size" validate:"required,gt=0,lte=100"`
	UserId      int64 `form:"user_id"`
	SubscribeId int64 `form:"subscribe_id"`
	StartTime   int64 `form:"start_time"`
	EndTime     int64 `form:"end_time"`
}

type GetUserSubscribeTrafficLogsResponse struct {
	List  []TrafficLog `json:"list"`
	Total int64        `json:"total"`
}

type GetUserTicketDetailRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type GetUserTicketListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Status *uint8 `form:"status,omitempty"`
	Search string `form:"search,omitempty"`
}

type GetUserTicketListResponse struct {
	Total int64    `json:"total"`
	List  []Ticket `json:"list"`
}

type GiftLog struct {
	Type        uint16 `json:"type"`
	UserId      int64  `json:"user_id"`
	OrderNo     string `json:"order_no"`
	SubscribeId int64  `json:"subscribe_id"`
	Amount      int64  `json:"amount"`
	Balance     int64  `json:"balance"`
	Remark      string `json:"remark,omitempty"`
	Timestamp   int64  `json:"timestamp"`
}

type GoogleLoginCallbackRequest struct {
	Code  string `form:"code"`
	State string `form:"state"`
}

type HasMigrateSeverNodeResponse struct {
	HasMigrate bool `json:"has_migrate"`
}

type HeartbeatResponse struct {
	Status    bool   `json:"status"`
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Hysteria2 struct {
	Port           int            `json:"port" validate:"required"`
	HopPorts       string         `json:"hop_ports" validate:"required"`
	HopInterval    int            `json:"hop_interval" validate:"required"`
	ObfsPassword   string         `json:"obfs_password" validate:"required"`
	SecurityConfig SecurityConfig `json:"security_config"`
}

type InviteConfig struct {
	ForcedInvite       bool  `json:"forced_invite"`
	ReferralPercentage int64 `json:"referral_percentage"`
	OnlyFirstPurchase  bool  `json:"only_first_purchase"`
}

type KickOfflineRequest struct {
	Id int64 `json:"id"`
}

type LogResponse struct {
	List interface{} `json:"list"`
}

type LogSetting struct {
	AutoClear *bool `json:"auto_clear"`
	ClearDays int64 `json:"clear_days"`
}

type LoginLog struct {
	UserId    int64  `json:"user_id"`
	Method    string `json:"method"`
	LoginIP   string `json:"login_ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
	Timestamp int64  `json:"timestamp"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type MessageLog struct {
	Id        int64       `json:"id"`
	Type      uint8       `json:"type"`
	Platform  string      `json:"platform"`
	To        string      `json:"to"`
	Subject   string      `json:"subject"`
	Content   interface{} `json:"content"`
	Status    uint8       `json:"status"`
	CreatedAt int64       `json:"created_at"`
}

type MigrateServerNodeResponse struct {
	Succee  uint64 `json:"succee"`
	Fail    uint64 `json:"fail"`
	Message string `json:"message,omitempty"`
}

type MobileAuthenticateConfig struct {
	Enable          bool     `json:"enable"`
	EnableWhitelist bool     `json:"enable_whitelist"`
	Whitelist       []string `json:"whitelist"`
}

type ModuleConfig struct {
	Secret         string `json:"secret"`          // 通讯密钥
	ServiceName    string `json:"service_name"`    // 服务名称
	ServiceVersion string `json:"service_version"` // 服务版本
}

type Node struct {
	Id        int64    `json:"id"`
	Name      string   `json:"name"`
	Tags      []string `json:"tags"`
	Port      uint16   `json:"port"`
	Address   string   `json:"address"`
	ServerId  int64    `json:"server_id"`
	Protocol  string   `json:"protocol"`
	Enabled   *bool    `json:"enabled"`
	Sort      int      `json:"sort,omitempty"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

type NodeConfig struct {
	NodeSecret             string         `json:"node_secret"`
	NodePullInterval       int64          `json:"node_pull_interval"`
	NodePushInterval       int64          `json:"node_push_interval"`
	TrafficReportThreshold int64          `json:"traffic_report_threshold"`
	IPStrategy             string         `json:"ip_strategy"`
	DNS                    []NodeDNS      `json:"dns"`
	Block                  []string       `json:"block"`
	Outbound               []NodeOutbound `json:"outbound"`
}

type NodeDNS struct {
	Proto   string   `json:"proto"`
	Address string   `json:"address"`
	Domains []string `json:"domains"`
}

type NodeOutbound struct {
	Name                 string   `json:"name"`
	Protocol             string   `json:"protocol"`
	Address              string   `json:"address"`
	Port                 int64    `json:"port"`
	User                 string   `json:"user,omitempty"`
	Password             string   `json:"password"`
	UUID                 string   `json:"uuid,omitempty"`
	Cipher               string   `json:"cipher,omitempty"`
	Security             string   `json:"security,omitempty"`
	SNI                  string   `json:"sni,omitempty"`
	AllowInsecure        bool     `json:"allow_insecure,omitempty"`
	Fingerprint          string   `json:"fingerprint,omitempty"`
	Transport            string   `json:"transport,omitempty"`
	Host                 string   `json:"host,omitempty"`
	Path                 string   `json:"path,omitempty"`
	ServiceName          string   `json:"service_name,omitempty"`
	Flow                 string   `json:"flow,omitempty"`
	UoT                  bool     `json:"uot,omitempty"`
	UoTVersion           int      `json:"uot_version,omitempty"`
	CongestionController string   `json:"congestion_controller,omitempty"`
	UDPStream            bool     `json:"udp_stream,omitempty"`
	ReduceRtt            bool     `json:"reduce_rtt,omitempty"`
	Heartbeat            int      `json:"heartbeat,omitempty"`
	RealityPublicKey     string   `json:"reality_public_key,omitempty"`
	RealityShortId       string   `json:"reality_short_id,omitempty"`
	SpiderX              string   `json:"spider_x,omitempty"`
	Settings             string   `json:"settings,omitempty"`
	StreamSettings       string   `json:"stream_settings,omitempty"`
	Rules                []string `json:"rules"`
}

type ServerNodeConfigValues struct {
	IPStrategy string         `json:"ip_strategy"`
	DNS        []NodeDNS      `json:"dns"`
	Block      []string       `json:"block"`
	Outbound   []NodeOutbound `json:"outbound"`
}

type ServerNodeConfigOverride struct {
	InheritIPStrategy bool   `json:"inherit_ip_strategy"`
	IPStrategy        string `json:"ip_strategy"`

	InheritDNS bool      `json:"inherit_dns"`
	DNS        []NodeDNS `json:"dns"`

	InheritBlock bool     `json:"inherit_block"`
	Block        []string `json:"block"`

	InheritOutbound bool           `json:"inherit_outbound"`
	Outbound        []NodeOutbound `json:"outbound"`
}

type NodeRelay struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Prefix string `json:"prefix"`
}

type OAthLoginRequest struct {
	Method   string `json:"method" validate:"required"` // google, facebook, apple, telegram, github etc.
	Redirect string `json:"redirect"`
}

type OAuthLoginGetTokenRequest struct {
	Method   string      `json:"method" validate:"required"` // google, facebook, apple, telegram, github etc.
	Callback interface{} `json:"callback" validate:"required"`
}

type OAuthLoginResponse struct {
	Redirect string `json:"redirect"`
}

type OnlineUser struct {
	SID int64  `json:"uid"`
	IP  string `json:"ip"`
}

type OnlineUsersRequest struct {
	ServerCommon
	Users []OnlineUser `json:"users"`
}

type Order struct {
	Id             int64         `json:"id"`
	UserId         int64         `json:"user_id"`
	OrderNo        string        `json:"order_no"`
	Type           uint8         `json:"type"`
	Quantity       int64         `json:"quantity"`
	Price          int64         `json:"price"`
	Amount         int64         `json:"amount"`
	GiftAmount     int64         `json:"gift_amount"`
	Discount       int64         `json:"discount"`
	Coupon         string        `json:"coupon"`
	CouponDiscount int64         `json:"coupon_discount"`
	Commission     int64         `json:"commission,omitempty"`
	Payment        PaymentMethod `json:"payment"`
	FeeAmount      int64         `json:"fee_amount"`
	TradeNo        string        `json:"trade_no"`
	Status         uint8         `json:"status"`
	SubscribeId    int64         `json:"subscribe_id"`
	CreatedAt      int64         `json:"created_at"`
	UpdatedAt      int64         `json:"updated_at"`
}

type OrderDetail struct {
	Id             int64         `json:"id"`
	UserId         int64         `json:"user_id"`
	OrderNo        string        `json:"order_no"`
	Type           uint8         `json:"type"`
	Quantity       int64         `json:"quantity"`
	Price          int64         `json:"price"`
	Amount         int64         `json:"amount"`
	GiftAmount     int64         `json:"gift_amount"`
	Discount       int64         `json:"discount"`
	Coupon         string        `json:"coupon"`
	CouponDiscount int64         `json:"coupon_discount"`
	Commission     int64         `json:"commission,omitempty"`
	Payment        PaymentMethod `json:"payment"`
	Method         string        `json:"method"`
	FeeAmount      int64         `json:"fee_amount"`
	TradeNo        string        `json:"trade_no"`
	Status         uint8         `json:"status"`
	SubscribeId    int64         `json:"subscribe_id"`
	Subscribe      Subscribe     `json:"subscribe"`
	CreatedAt      int64         `json:"created_at"`
	UpdatedAt      int64         `json:"updated_at"`
}

type OrdersStatistics struct {
	Date               string             `json:"date,omitempty"`
	AmountTotal        int64              `json:"amount_total"`
	NewOrderAmount     int64              `json:"new_order_amount"`
	RenewalOrderAmount int64              `json:"renewal_order_amount"`
	List               []OrdersStatistics `json:"list,omitempty"`
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

type PortalPurchaseRequest struct {
	AuthType       string `json:"auth_type"`
	Identifier     string `json:"identifier"`
	Password       string `json:"password,omitempty"`
	Payment        int64  `json:"payment"`
	SubscribeId    int64  `json:"subscribe_id"`
	Quantity       int64  `json:"quantity"`
	Coupon         string `json:"coupon,omitempty"`
	InviteCode     string `json:"invite_code,omitempty"`
	TurnstileToken string `json:"turnstile_token,omitempty"`
}

type PortalPurchaseResponse struct {
	OrderNo string `json:"order_no"`
}

type PreOrderResponse struct {
	Price          int64  `json:"price"`
	Amount         int64  `json:"amount"`
	Discount       int64  `json:"discount"`
	GiftAmount     int64  `json:"gift_amount"`
	Coupon         string `json:"coupon"`
	CouponDiscount int64  `json:"coupon_discount"`
	FeeAmount      int64  `json:"fee_amount"`
}

type PrePurchaseOrderRequest struct {
	Payment     int64  `json:"payment,omitempty"`
	SubscribeId int64  `json:"subscribe_id"`
	Quantity    int64  `json:"quantity"`
	Coupon      string `json:"coupon,omitempty"`
}

type PrePurchaseOrderResponse struct {
	Price          int64  `json:"price"`
	Amount         int64  `json:"amount"`
	Discount       int64  `json:"discount"`
	Coupon         string `json:"coupon"`
	CouponDiscount int64  `json:"coupon_discount"`
	FeeAmount      int64  `json:"fee_amount"`
}

type PreRenewalOrderResponse struct {
	OrderNo string `json:"orderNo"`
}

type PreUnsubscribeRequest struct {
	Id int64 `json:"id"`
}

type PreUnsubscribeResponse struct {
	DeductionAmount int64 `json:"deduction_amount"`
}

type PreViewNodeMultiplierResponse struct {
	CurrentTime string  `json:"current_time"`
	Ratio       float32 `json:"ratio"`
}

type PreviewSubscribeTemplateRequest struct {
	Id int64 `form:"id"`
}

type PreviewSubscribeTemplateResponse struct {
	Template string `json:"template"` // 预览的模板内容
}

type PrivacyPolicyConfig struct {
	PrivacyPolicy string `json:"privacy_policy"`
}

type Protocol struct {
	Type                    string  `json:"type"`
	Port                    uint16  `json:"port"`
	Enable                  bool    `json:"enable"`
	Security                string  `json:"security,omitempty"`
	SNI                     string  `json:"sni,omitempty"`
	AllowInsecure           bool    `json:"allow_insecure,omitempty"`
	Fingerprint             string  `json:"fingerprint,omitempty"`
	RealityServerAddr       string  `json:"reality_server_addr,omitempty"`
	RealityServerPort       int     `json:"reality_server_port,omitempty"`
	RealityPrivateKey       string  `json:"reality_private_key,omitempty"`
	RealityPublicKey        string  `json:"reality_public_key,omitempty"`
	RealityShortId          string  `json:"reality_short_id,omitempty"`
	Transport               string  `json:"transport,omitempty"`
	Host                    string  `json:"host,omitempty"`
	Path                    string  `json:"path,omitempty"`
	ServiceName             string  `json:"service_name,omitempty"`
	Cipher                  string  `json:"cipher,omitempty"`
	ServerKey               string  `json:"server_key,omitempty"`
	Flow                    string  `json:"flow,omitempty"`
	UoT                     bool    `json:"uot,omitempty"`                   // UDP over TCP
	UoTVersion              int     `json:"uot_version,omitempty"`           // UoT version (1 or 2)
	AcceptProxyProtocol     bool    `json:"accept_proxy_protocol,omitempty"` // accept proxy protocol
	HopPorts                string  `json:"hop_ports,omitempty"`
	HopInterval             int     `json:"hop_interval,omitempty"`
	ObfsPassword            string  `json:"obfs_password,omitempty"`
	DisableSNI              bool    `json:"disable_sni,omitempty"`
	ReduceRtt               bool    `json:"reduce_rtt,omitempty"`
	UDPRelayMode            string  `json:"udp_relay_mode,omitempty"`
	CongestionController    string  `json:"congestion_controller,omitempty"`
	Multiplex               string  `json:"multiplex,omitempty"`                 // mux, eg: off/low/medium/high
	PaddingScheme           string  `json:"padding_scheme,omitempty"`            // padding scheme
	UpMbps                  int     `json:"up_mbps,omitempty"`                   // upload speed limit
	DownMbps                int     `json:"down_mbps,omitempty"`                 // download speed limit
	Obfs                    string  `json:"obfs,omitempty"`                      // obfs, 'none', 'http', 'tls'
	ObfsHost                string  `json:"obfs_host,omitempty"`                 // obfs host
	ObfsPath                string  `json:"obfs_path,omitempty"`                 // obfs path
	XhttpMode               string  `json:"xhttp_mode,omitempty"`                // xhttp mode
	XhttpExtra              string  `json:"xhttp_extra,omitempty"`               // xhttp extra path
	Encryption              string  `json:"encryption,omitempty"`                // encryption，'none', 'mlkem768x25519plus'
	EncryptionMode          string  `json:"encryption_mode,omitempty"`           // encryption mode，'native', 'xorpub', 'random'
	EncryptionRtt           string  `json:"encryption_rtt,omitempty"`            // encryption rtt，'0rtt', '1rtt'
	EncryptionTicket        string  `json:"encryption_ticket,omitempty"`         // encryption ticket
	EncryptionServerPadding string  `json:"encryption_server_padding,omitempty"` // encryption server padding
	EncryptionPrivateKey    string  `json:"encryption_private_key,omitempty"`    // encryption private key
	EncryptionClientPadding string  `json:"encryption_client_padding,omitempty"` // encryption client padding
	EncryptionPassword      string  `json:"encryption_password,omitempty"`       // encryption password
	EchEnable               bool    `json:"ech_enable,omitempty"`                // ECH enable
	EchServerName           string  `json:"ech_server_name,omitempty"`           // ECH server name
	Ratio                   float64 `json:"ratio,omitempty"`                     // Traffic ratio, default is 1
	CertMode                string  `json:"cert_mode,omitempty"`                 // Certificate mode, `none`｜`http`｜`dns`｜`self`
	CertDNSProvider         string  `json:"cert_dns_provider,omitempty"`         // DNS provider for certificate
	CertDNSEnv              string  `json:"cert_dns_env,omitempty"`              // Environment for DNS provider
}

type PubilcRegisterConfig struct {
	StopRegister            bool  `json:"stop_register"`
	EnableIpRegisterLimit   bool  `json:"enable_ip_register_limit"`
	IpRegisterLimit         int64 `json:"ip_register_limit"`
	IpRegisterLimitDuration int64 `json:"ip_register_limit_duration"`
}

type PubilcVerifyCodeConfig struct {
	VerifyCodeInterval int64 `json:"verify_code_interval"`
}

type PurchaseOrderRequest struct {
	SubscribeId int64  `json:"subscribe_id"`
	Quantity    int64  `json:"quantity" validate:"required,gt=0,lte=1000"`
	Payment     int64  `json:"payment,omitempty"`
	Coupon      string `json:"coupon,omitempty"`
}

type PurchaseOrderResponse struct {
	OrderNo string `json:"order_no"`
}

type QueryAnnouncementRequest struct {
	Page   int   `form:"page" validate:"required,gt=0"`
	Size   int   `form:"size" validate:"required,gt=0,lte=100"`
	Pinned *bool `form:"pinned"`
	Popup  *bool `form:"popup"`
}

type QueryAnnouncementResponse struct {
	Total int64          `json:"total"`
	List  []Announcement `json:"announcements"`
}

type QueryDocumentDetailRequest struct {
	Id int64 `form:"id" validate:"required"`
}

type QueryDocumentListResponse struct {
	Total int64      `json:"total"`
	List  []Document `json:"list"`
}

type QueryIPLocationRequest struct {
	IP string `form:"ip" validate:"required"`
}

type QueryIPLocationResponse struct {
	Country string `json:"country"`
	Region  string `json:"region,omitempty"`
	City    string `json:"city"`
}

type QueryNodeTagResponse struct {
	Tags []string `json:"tags"`
}

type QueryOrderDetailRequest struct {
	OrderNo string `form:"order_no" validate:"required"`
}

type QueryOrderListRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type QueryOrderListResponse struct {
	Total int64         `json:"total"`
	List  []OrderDetail `json:"list"`
}

type QueryPurchaseOrderRequest struct {
	AuthType   string `form:"auth_type"`
	Identifier string `form:"identifier"`
	OrderNo    string `form:"order_no"`
}

type QueryPurchaseOrderResponse struct {
	OrderNo        string        `json:"order_no"`
	Subscribe      Subscribe     `json:"subscribe"`
	Quantity       int64         `json:"quantity"`
	Price          int64         `json:"price"`
	Amount         int64         `json:"amount"`
	Discount       int64         `json:"discount"`
	Coupon         string        `json:"coupon"`
	CouponDiscount int64         `json:"coupon_discount"`
	FeeAmount      int64         `json:"fee_amount"`
	Payment        PaymentMethod `json:"payment"`
	Status         uint8         `json:"status"`
	CreatedAt      int64         `json:"created_at"`
	Token          string        `json:"token,omitempty"`
}

type QueryQuotaTaskListRequest struct {
	Page   int    `form:"page" validate:"required,gt=0"`
	Size   int    `form:"size" validate:"required,gt=0,lte=100"`
	Status *uint8 `form:"status,omitempty"`
}

type QueryQuotaTaskListResponse struct {
	Total int64       `json:"total"`
	List  []QuotaTask `json:"list"`
}

type QueryQuotaTaskPreCountRequest struct {
	Subscribers []int64 `json:"subscribers"`
	IsActive    *bool   `json:"is_active"`
	StartTime   int64   `json:"start_time"`
	EndTime     int64   `json:"end_time"`
}

type QueryQuotaTaskPreCountResponse struct {
	Count int64 `json:"count"`
}

type QueryQuotaTaskStatusRequest struct {
	Id int64 `json:"id"`
}

type QueryQuotaTaskStatusResponse struct {
	Status  uint8  `json:"status"`
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Errors  string `json:"errors"`
}

type QueryServerConfigRequest struct {
	ServerID  int64    `path:"server_id"`
	SecretKey string   `form:"secret_key"`
	Protocols []string `form:"protocols,omitempty"`
}

type QueryServerConfigResponse struct {
	TrafficReportThreshold int64          `json:"traffic_report_threshold"`
	PushInterval           int64          `json:"push_interval"`
	PullInterval           int64          `json:"pull_interval"`
	IPStrategy             string         `json:"ip_strategy"`
	DNS                    []NodeDNS      `json:"dns"`
	Block                  []string       `json:"block"`
	Outbound               []NodeOutbound `json:"outbound"`
	Protocols              []Protocol     `json:"protocols"`
	Total                  int64          `json:"total"`
}

type GetServerNodeConfigRequest struct {
	ServerID int64 `form:"server_id" validate:"required"`
}

type GetServerNodeConfigResponse struct {
	Global    ServerNodeConfigValues   `json:"global"`
	Override  ServerNodeConfigOverride `json:"override"`
	Effective ServerNodeConfigValues   `json:"effective"`
}

type UpdateServerNodeConfigRequest struct {
	ServerID int64 `json:"server_id" validate:"required"`
	ServerNodeConfigOverride
}

type QuerySubscribeGroupListResponse struct {
	List  []SubscribeGroup `json:"list"`
	Total int64            `json:"total"`
}

type QuerySubscribeListRequest struct {
	Language string `form:"language"`
}

type QuerySubscribeListResponse struct {
	List  []Subscribe `json:"list"`
	Total int64       `json:"total"`
}

type QueryUserAffiliateCountResponse struct {
	Registers       int64 `json:"registers"`
	TotalCommission int64 `json:"total_commission"`
}

type QueryUserAffiliateListRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type QueryUserAffiliateListResponse struct {
	List  []UserAffiliate `json:"list"`
	Total int64           `json:"total"`
}

type QueryUserBalanceLogListResponse struct {
	List  []BalanceLog `json:"list"`
	Total int64        `json:"total"`
}

type QueryUserCommissionLogListRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type QueryUserCommissionLogListResponse struct {
	List  []CommissionLog `json:"list"`
	Total int64           `json:"total"`
}

type QueryUserSubscribeListResponse struct {
	List  []UserSubscribe `json:"list"`
	Total int64           `json:"total"`
}

type QueryUserSubscribeNodeListResponse struct {
	List []UserSubscribeInfo `json:"list"`
}

type QueryWithdrawalLogListRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type QueryWithdrawalLogListResponse struct {
	List  []WithdrawalLog `json:"list"`
	Total int64           `json:"total"`
}

type QuotaTask struct {
	Id           int64   `json:"id"`
	Subscribers  []int64 `json:"subscribers"`
	IsActive     *bool   `json:"is_active"`
	StartTime    int64   `json:"start_time"`
	EndTime      int64   `json:"end_time"`
	ResetTraffic bool    `json:"reset_traffic"`
	Days         uint64  `json:"days"`
	GiftType     uint8   `json:"gift_type"`
	GiftValue    uint64  `json:"gift_value"`
	Objects      []int64 `json:"objects"` // UserSubscribe IDs
	Status       uint8   `json:"status"`
	Total        int64   `json:"total"`
	Current      int64   `json:"current"`
	Errors       string  `json:"errors"`
	CreatedAt    int64   `json:"created_at"`
	UpdatedAt    int64   `json:"updated_at"`
}

type RechargeOrderRequest struct {
	Amount  int64 `json:"amount" validate:"required,gt=0,lte=2000000000"`
	Payment int64 `json:"payment"`
}

type RechargeOrderResponse struct {
	OrderNo string `json:"order_no"`
}

type RegisterConfig struct {
	StopRegister            bool   `json:"stop_register"`
	EnableTrial             bool   `json:"enable_trial"`
	TrialSubscribe          int64  `json:"trial_subscribe"`
	TrialTime               int64  `json:"trial_time"`
	TrialTimeUnit           string `json:"trial_time_unit"`
	EnableIpRegisterLimit   bool   `json:"enable_ip_register_limit"`
	IpRegisterLimit         int64  `json:"ip_register_limit"`
	IpRegisterLimitDuration int64  `json:"ip_register_limit_duration"`
}

type RegisterLog struct {
	UserId     int64  `json:"user_id"`
	AuthMethod string `json:"auth_method"`
	Identifier string `json:"identifier"`
	RegisterIP string `json:"register_ip"`
	UserAgent  string `json:"user_agent"`
	Timestamp  int64  `json:"timestamp"`
}

type RenewalOrderRequest struct {
	UserSubscribeID int64  `json:"user_subscribe_id"`
	Quantity        int64  `json:"quantity" validate:"lte=1000"`
	Payment         int64  `json:"payment"`
	Coupon          string `json:"coupon,omitempty"`
}

type RenewalOrderResponse struct {
	OrderNo string `json:"order_no"`
}

type ResetAllSubscribeTokenResponse struct {
	Success bool `json:"success"`
}

type ResetPasswordRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email" validate:"required"`
	Password   string `json:"password" validate:"required"`
	Code       string `json:"code,optional"`
	IP         string `header:"X-Original-Forwarded-For"`
	UserAgent  string `header:"User-Agent"`
	LoginType  string `header:"Login-Type"`
	CfToken    string `json:"cf_token,optional"`
}

type ResetSortRequest struct {
	Sort []SortItem `json:"sort"`
}

type ResetSubscribeLog struct {
	Type            uint16 `json:"type"`
	UserId          int64  `json:"user_id"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
	OrderNo         string `json:"order_no,omitempty"`
	Timestamp       int64  `json:"timestamp"`
}

type ResetSubscribeTrafficLog struct {
	Id              int64  `json:"id"`
	Type            uint16 `json:"type"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
	OrderNo         string `json:"order_no,omitempty"`
	Timestamp       int64  `json:"timestamp"`
}

type ResetTrafficOrderRequest struct {
	UserSubscribeID int64 `json:"user_subscribe_id"`
	Payment         int64 `json:"payment"`
}

type ResetTrafficOrderResponse struct {
	OrderNo string `json:"order_no"`
}

type ResetUserSubscribeTokenRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id"`
}

type ResetUserSubscribeTrafficRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id"`
}

type RevenueStatisticsResponse struct {
	Today   OrdersStatistics `json:"today"`
	Monthly OrdersStatistics `json:"monthly"`
	All     OrdersStatistics `json:"all"`
}

type SecurityConfig struct {
	SNI               string `json:"sni"`
	AllowInsecure     *bool  `json:"allow_insecure"`
	Fingerprint       string `json:"fingerprint"`
	RealityServerAddr string `json:"reality_server_addr"`
	RealityServerPort int    `json:"reality_server_port"`
	RealityPrivateKey string `json:"reality_private_key"`
	RealityPublicKey  string `json:"reality_public_key"`
	RealityShortId    string `json:"reality_short_id"`
}

type SendCodeRequest struct {
	Email string `json:"email" validate:"required"`
	Type  uint8  `json:"type" validate:"required"`
}

type SendCodeResponse struct {
	Code   string `json:"code,omitempty"`
	Status bool   `json:"status"`
}

type SendSmsCodeRequest struct {
	Type              uint8  `json:"type" validate:"required"`
	Telephone         string `json:"telephone" validate:"required"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
}

type Server struct {
	Id             int64        `json:"id"`
	Name           string       `json:"name"`
	Country        string       `json:"country"`
	City           string       `json:"city"`
	Address        string       `json:"address"`
	Sort           int          `json:"sort"`
	Protocols      []Protocol   `json:"protocols"`
	LastReportedAt int64        `json:"last_reported_at"`
	Status         ServerStatus `json:"status"`
	CreatedAt      int64        `json:"created_at"`
	UpdatedAt      int64        `json:"updated_at"`
}

type ServerBasic struct {
	PushInterval int64 `json:"push_interval"`
	PullInterval int64 `json:"pull_interval"`
}

type ServerCommon struct {
	Protocol  string `form:"protocol"`
	ServerId  int64  `form:"server_id"`
	SecretKey string `form:"secret_key"`
}

type ServerGroup struct {
	Id          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type ServerOnlineIP struct {
	IP       string `json:"ip"`
	Protocol string `json:"protocol"`
}

type ServerOnlineUser struct {
	IP          []ServerOnlineIP `json:"ip"`
	UserId      int64            `json:"user_id"`
	Subscribe   string           `json:"subscribe"`
	SubscribeId int64            `json:"subscribe_id"`
	Traffic     int64            `json:"traffic"`
	ExpiredAt   int64            `json:"expired_at"`
}

type ServerPushStatusRequest struct {
	ServerCommon
	Cpu       float64 `json:"cpu"`
	Mem       float64 `json:"mem"`
	Disk      float64 `json:"disk"`
	UpdatedAt int64   `json:"updated_at"`
}

type ServerPushUserTrafficRequest struct {
	ServerCommon
	Traffic []UserTraffic `json:"traffic"`
}

type ServerRuleGroup struct {
	Id        int64    `json:"id"`
	Icon      string   `json:"icon"`
	Name      string   `json:"name" validate:"required"`
	Type      string   `json:"type"`
	Tags      []string `json:"tags"`
	Rules     string   `json:"rules"`
	Enable    bool     `json:"enable"`
	Default   bool     `json:"default"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

type ServerStatus struct {
	Cpu      float64            `json:"cpu"`
	Mem      float64            `json:"mem"`
	Disk     float64            `json:"disk"`
	Protocol string             `json:"protocol"`
	Online   []ServerOnlineUser `json:"online"`
	Status   string             `json:"status"`
}

type ServerTotalDataResponse struct {
	OnlineUsers                   int64               `json:"online_users"`
	OnlineServers                 int64               `json:"online_servers"`
	OfflineServers                int64               `json:"offline_servers"`
	TodayUpload                   int64               `json:"today_upload"`
	TodayDownload                 int64               `json:"today_download"`
	MonthlyUpload                 int64               `json:"monthly_upload"`
	MonthlyDownload               int64               `json:"monthly_download"`
	UpdatedAt                     int64               `json:"updated_at"`
	ServerTrafficRankingToday     []ServerTrafficData `json:"server_traffic_ranking_today"`
	ServerTrafficRankingYesterday []ServerTrafficData `json:"server_traffic_ranking_yesterday"`
	UserTrafficRankingToday       []UserTrafficData   `json:"user_traffic_ranking_today"`
	UserTrafficRankingYesterday   []UserTrafficData   `json:"user_traffic_ranking_yesterday"`
}

type ServerTrafficData struct {
	ServerId int64  `json:"server_id"`
	Name     string `json:"name"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
}

type ServerTrafficLog struct {
	ServerId int64  `json:"server_id"` // Server ID
	Upload   int64  `json:"upload"`    // Upload traffic in bytes
	Download int64  `json:"download"`  // Download traffic in bytes
	Total    int64  `json:"total"`     // Total traffic in bytes (Upload + Download)
	Date     string `json:"date"`      // Date in YYYY-MM-DD format
	Details  bool   `json:"details"`   // Whether to show detailed traffic
}

type ServerUser struct {
	Id          int64  `json:"id"`
	UUID        string `json:"uuid"`
	SpeedLimit  int64  `json:"speed_limit"`
	DeviceLimit int64  `json:"device_limit"`
}

type SetNodeMultiplierRequest struct {
	Periods []TimePeriod `json:"periods"`
}

type Shadowsocks struct {
	Method    string `json:"method" validate:"required"`
	Port      int    `json:"port" validate:"required"`
	ServerKey string `json:"server_key"`
}

type ShadowsocksProtocol struct {
	Port   int    `json:"port"`
	Method string `json:"method"`
}

type SiteConfig struct {
	Host       string `json:"host"`
	SiteName   string `json:"site_name"`
	SiteDesc   string `json:"site_desc"`
	SiteLogo   string `json:"site_logo"`
	Keywords   string `json:"keywords"`
	CustomHTML string `json:"custom_html"`
	CustomData string `json:"custom_data"`
}

type SiteCustomDataContacts struct {
	Email     string `json:"email"`
	Telephone string `json:"telephone"`
	Address   string `json:"address"`
}

type SortItem struct {
	Id   int64 `json:"id" validate:"required"`
	Sort int64 `json:"sort" validate:"required"`
}

type StopBatchSendEmailTaskRequest struct {
	Id int64 `json:"id"`
}

type StripePayment struct {
	Method         string `json:"method"`
	ClientSecret   string `json:"client_secret"`
	PublishableKey string `json:"publishable_key"`
}

type Subscribe struct {
	Id                int64               `json:"id"`
	Name              string              `json:"name"`
	Language          string              `json:"language"`
	Description       string              `json:"description"`
	UnitPrice         int64               `json:"unit_price"`
	UnitTime          string              `json:"unit_time"`
	Discount          []SubscribeDiscount `json:"discount"`
	Replacement       int64               `json:"replacement"`
	Inventory         int64               `json:"inventory"`
	Traffic           int64               `json:"traffic"`
	SpeedLimit        int64               `json:"speed_limit"`
	DeviceLimit       int64               `json:"device_limit"`
	Quota             int64               `json:"quota"`
	Nodes             StringInt64Slice    `json:"nodes"`
	NodeTags          []string            `json:"node_tags"`
	Show              bool                `json:"show"`
	Sell              bool                `json:"sell"`
	Sort              int64               `json:"sort"`
	DeductionRatio    int64               `json:"deduction_ratio"`
	AllowDeduction    bool                `json:"allow_deduction"`
	ResetCycle        int64               `json:"reset_cycle"`
	RenewalReset      bool                `json:"renewal_reset"`
	ShowOriginalPrice bool                `json:"show_original_price"`
	CreatedAt         int64               `json:"created_at"`
	UpdatedAt         int64               `json:"updated_at"`
}

type SubscribeApplication struct {
	Id                int64        `json:"id"`
	Name              string       `json:"name"`
	Description       string       `json:"description,omitempty"`
	Icon              string       `json:"icon,omitempty"`
	Scheme            string       `json:"scheme,omitempty"`
	UserAgent         string       `json:"user_agent"`
	IsDefault         bool         `json:"is_default"`
	SubscribeTemplate string       `json:"template"`
	OutputFormat      string       `json:"output_format"`
	DownloadLink      DownloadLink `json:"download_link,omitempty"`
	CreatedAt         int64        `json:"created_at"`
	UpdatedAt         int64        `json:"updated_at"`
}

type SubscribeClient struct {
	Id           int64        `json:"id"`
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	Icon         string       `json:"icon,omitempty"`
	Scheme       string       `json:"scheme,omitempty"`
	IsDefault    bool         `json:"is_default"`
	DownloadLink DownloadLink `json:"download_link,omitempty"`
}

type SubscribeConfig struct {
	SingleModel     bool   `json:"single_model"`
	SubscribePath   string `json:"subscribe_path"`
	SubscribeDomain string `json:"subscribe_domain"`
	PanDomain       bool   `json:"pan_domain"`
	UserAgentLimit  bool   `json:"user_agent_limit"`
	UserAgentList   string `json:"user_agent_list"`
	ShowTutorial    bool   `json:"show_tutorial"`
}

type SubscribeDiscount struct {
	Quantity int64   `json:"quantity"`
	Discount float64 `json:"discount"`
}

type SubscribeGroup struct {
	Id          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type SubscribeItem struct {
	Subscribe
	Sold int64 `json:"sold"`
}

type SubscribeLog struct {
	UserId          int64  `json:"user_id"`
	Token           string `json:"token"`
	UserAgent       string `json:"user_agent"`
	ClientIP        string `json:"client_ip"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
	Timestamp       int64  `json:"timestamp"`
}

type SubscribeSortRequest struct {
	Sort []SortItem `json:"sort"`
}

type SubscribeType struct {
	SubscribeTypes []string `json:"subscribe_types"`
}

type TelegramConfig struct {
	TelegramBotToken      string `json:"telegram_bot_token"`
	TelegramGroupUrl      string `json:"telegram_group_url"`
	TelegramNotify        bool   `json:"telegram_notify"`
	TelegramWebHookDomain string `json:"telegram_web_hook_domain"`
}

type TelephoneCheckUserRequest struct {
	Telephone         string `form:"telephone" validate:"required"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
}

type TelephoneCheckUserResponse struct {
	Exist bool `json:"exist"`
}

type TelephoneLoginRequest struct {
	Identifier        string `json:"identifier"`
	Telephone         string `json:"telephone" validate:"required"`
	TelephoneCode     string `json:"telephone_code"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
	Password          string `json:"password"`
	IP                string `header:"X-Original-Forwarded-For"`
	UserAgent         string `header:"User-Agent"`
	LoginType         string `header:"Login-Type"`
	CfToken           string `json:"cf_token,optional"`
}

type TelephoneRegisterRequest struct {
	Identifier        string `json:"identifier"`
	Telephone         string `json:"telephone" validate:"required"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
	Password          string `json:"password" validate:"required"`
	Invite            string `json:"invite,optional"`
	Code              string `json:"code,optional"`
	IP                string `header:"X-Original-Forwarded-For"`
	UserAgent         string `header:"User-Agent"`
	LoginType         string `header:"Login-Type,optional"`
	CfToken           string `json:"cf_token,optional"`
}

type TelephoneResetPasswordRequest struct {
	Identifier        string `json:"identifier"`
	Telephone         string `json:"telephone" validate:"required"`
	TelephoneAreaCode string `json:"telephone_area_code" validate:"required"`
	Password          string `json:"password" validate:"required"`
	Code              string `json:"code,optional"`
	IP                string `header:"X-Original-Forwarded-For"`
	UserAgent         string `header:"User-Agent"`
	LoginType         string `header:"Login-Type,optional"`
	CfToken           string `json:"cf_token,optional"`
}

type TestEmailSendRequest struct {
	Email string `json:"email" validate:"required"`
}

type TestSmsSendRequest struct {
	AreaCode  string `json:"area_code" validate:"required"`
	Telephone string `json:"telephone" validate:"required"`
}

type Ticket struct {
	Id          int64    `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	UserId      int64    `json:"user_id"`
	Follows     []Follow `json:"follow,omitempty"`
	Status      uint8    `json:"status"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

type TicketWaitRelpyResponse struct {
	Count int64 `json:"count"`
}

type TimePeriod struct {
	StartTime  string  `json:"start_time"`
	EndTime    string  `json:"end_time"`
	Multiplier float32 `json:"multiplier"`
}

type ToggleNodeStatusRequest struct {
	Id     int64 `json:"id"`
	Enable *bool `json:"enable"`
}

type ToggleUserSubscribeStatusRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id"`
}

type TosConfig struct {
	TosContent string `json:"tos_content"`
}

type TrafficLog struct {
	Id          int64 `json:"id"`
	ServerId    int64 `json:"server_id"`
	UserId      int64 `json:"user_id"`
	SubscribeId int64 `json:"subscribe_id"`
	Download    int64 `json:"download"`
	Upload      int64 `json:"upload"`
	Timestamp   int64 `json:"timestamp"`
}

type TrafficLogDetails struct {
	Id          int64 `json:"id"`
	ServerId    int64 `json:"server_id"`
	UserId      int64 `json:"user_id"`
	SubscribeId int64 `json:"subscribe_id"`
	Download    int64 `json:"download"`
	Upload      int64 `json:"upload"`
	Timestamp   int64 `json:"timestamp"`
}

type TransportConfig struct {
	Path        string `json:"path"`
	Host        string `json:"host"`
	ServiceName string `json:"service_name"`
}

type Trojan struct {
	Port            int             `json:"port" validate:"required"`
	Transport       string          `json:"transport" validate:"required"`
	TransportConfig TransportConfig `json:"transport_config"`
	Security        string          `json:"security" validate:"required"`
	SecurityConfig  SecurityConfig  `json:"security_config"`
}

type TrojanProtocol struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	EnableTLS *bool  `json:"enable_tls"`
	TLSConfig string `json:"tls_config"`
	Network   string `json:"network"`
	Transport string `json:"transport"`
}

type Tuic struct {
	Port                 int            `json:"port" validate:"required"`
	DisableSNI           bool           `json:"disable_sni"`
	ReduceRtt            bool           `json:"reduce_rtt"`
	UDPRelayMode         string         `json:"udp_relay_mode"`
	CongestionController string         `json:"congestion_controller"`
	SecurityConfig       SecurityConfig `json:"security_config"`
}

type UnbindDeviceRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type UnbindOAuthRequest struct {
	Method string `json:"method"`
}

type UnsubscribeRequest struct {
	Id int64 `json:"id"`
}

type UpdateAdsRequest struct {
	Id          int64  `json:"id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
	StartTime   int64  `json:"start_time"`
	EndTime     int64  `json:"end_time"`
	Status      int    `json:"status"`
}

type UpdateAnnouncementEnableRequest struct {
	Id     int64 `json:"id" validate:"required"`
	Enable *bool `json:"enable" validate:"required"`
}

type UpdateAnnouncementRequest struct {
	Id      int64  `json:"id" validate:"required"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Show    *bool  `json:"show"`
	Pinned  *bool  `json:"pinned"`
	Popup   *bool  `json:"popup"`
}

type UpdateAuthMethodConfigRequest struct {
	Id      int64       `json:"id"`
	Method  string      `json:"method"`
	Config  interface{} `json:"config"`
	Enabled *bool       `json:"enabled"`
}

type UpdateBindEmailRequest struct {
	Email string `json:"email" validate:"required"`
}

type UpdateBindMobileRequest struct {
	AreaCode string `json:"area_code" validate:"required"`
	Mobile   string `json:"mobile" validate:"required"`
	Code     string `json:"code" validate:"required"`
}

type UpdateCouponRequest struct {
	Id         int64   `json:"id" validate:"required"`
	Name       string  `json:"name" validate:"required"`
	Code       string  `json:"code,omitempty"`
	Count      int64   `json:"count,omitempty"`
	Type       uint8   `json:"type" validate:"required"`
	Discount   int64   `json:"discount" validate:"required"`
	StartTime  int64   `json:"start_time" validate:"required"`
	ExpireTime int64   `json:"expire_time" validate:"required"`
	UserLimit  int64   `json:"user_limit,omitempty"`
	Subscribe  []int64 `json:"subscribe,omitempty"`
	UsedCount  int64   `json:"used_count,omitempty"`
	Enable     *bool   `json:"enable,omitempty"`
}

type UpdateDocumentRequest struct {
	Id      int64    `json:"id" validate:"required"`
	Title   string   `json:"title" validate:"required"`
	Content string   `json:"content" validate:"required"`
	Tags    []string `json:"tags,omitempty" `
	Show    *bool    `json:"show"`
}

type UpdateNodeRequest struct {
	Id       int64    `json:"id"`
	Name     string   `json:"name"`
	Tags     []string `json:"tags,omitempty"`
	Port     uint16   `json:"port"`
	Address  string   `json:"address"`
	ServerId int64    `json:"server_id"`
	Protocol string   `json:"protocol"`
	Enabled  *bool    `json:"enabled"`
}

type UpdateOrderStatusRequest struct {
	Id        int64  `json:"id" validate:"required"`
	Status    uint8  `json:"status" validate:"required"`
	PaymentId int64  `json:"payment_id,omitempty"`
	TradeNo   string `json:"trade_no,omitempty"`
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

type UpdateServerRequest struct {
	Id        int64      `json:"id"`
	Name      string     `json:"name"`
	Country   string     `json:"country,omitempty"`
	City      string     `json:"city,omitempty"`
	Address   string     `json:"address"`
	Sort      int        `json:"sort,omitempty"`
	Protocols []Protocol `json:"protocols"`
}

type UpdateSubscribeApplicationRequest struct {
	Id                int64        `json:"id"`
	Name              string       `json:"name"`
	Description       string       `json:"description,omitempty"`
	Icon              string       `json:"icon,omitempty"`
	Scheme            string       `json:"scheme,omitempty"`
	UserAgent         string       `json:"user_agent"`
	IsDefault         bool         `json:"is_default"`
	SubscribeTemplate string       `json:"template"`
	OutputFormat      string       `json:"output_format"`
	DownloadLink      DownloadLink `json:"download_link,omitempty"`
}

type UpdateSubscribeGroupRequest struct {
	Id          int64  `json:"id" validate:"required"`
	Name        string `json:"name" validate:"required"`
	Description string `json:"description"`
}

type UpdateSubscribeRequest struct {
	Id                int64               `json:"id" validate:"required"`
	Name              string              `json:"name" validate:"required"`
	Language          string              `json:"language"`
	Description       string              `json:"description"`
	UnitPrice         int64               `json:"unit_price"`
	UnitTime          string              `json:"unit_time"`
	Discount          []SubscribeDiscount `json:"discount"`
	Replacement       int64               `json:"replacement"`
	Inventory         int64               `json:"inventory"`
	Traffic           int64               `json:"traffic"`
	SpeedLimit        int64               `json:"speed_limit"`
	DeviceLimit       int64               `json:"device_limit"`
	Quota             int64               `json:"quota"`
	Nodes             StringInt64Slice    `json:"nodes"`
	NodeTags          []string            `json:"node_tags"`
	Show              *bool               `json:"show"`
	Sell              *bool               `json:"sell"`
	Sort              int64               `json:"sort"`
	DeductionRatio    int64               `json:"deduction_ratio"`
	AllowDeduction    *bool               `json:"allow_deduction"`
	ResetCycle        int64               `json:"reset_cycle"`
	RenewalReset      *bool               `json:"renewal_reset"`
	ShowOriginalPrice bool                `json:"show_original_price"`
}

type UpdateTicketStatusRequest struct {
	Id     int64  `json:"id" validate:"required"`
	Status *uint8 `json:"status" validate:"required"`
}

type UpdateUserAuthMethodRequest struct {
	UserId         int64  `json:"user_id"`
	AuthType       string `json:"auth_type"`
	AuthIdentifier string `json:"auth_identifier"`
}

type UpdateUserBasiceInfoRequest struct {
	UserId             int64  `json:"user_id" validate:"required"`
	Password           string `json:"password"`
	Avatar             string `json:"avatar"`
	Balance            int64  `json:"balance"`
	Commission         int64  `json:"commission"`
	ReferralPercentage uint8  `json:"referral_percentage"`
	OnlyFirstPurchase  bool   `json:"only_first_purchase"`
	GiftAmount         int64  `json:"gift_amount"`
	Telegram           int64  `json:"telegram"`
	ReferCode          string `json:"refer_code"`
	RefererId          int64  `json:"referer_id"`
	Enable             bool   `json:"enable"`
	IsAdmin            bool   `json:"is_admin"`
}

type UpdateUserNotifyRequest struct {
	EnableBalanceNotify   *bool `json:"enable_balance_notify"`
	EnableLoginNotify     *bool `json:"enable_login_notify"`
	EnableSubscribeNotify *bool `json:"enable_subscribe_notify"`
	EnableTradeNotify     *bool `json:"enable_trade_notify"`
}

type UpdateUserNotifySettingRequest struct {
	UserId                int64 `json:"user_id" validate:"required"`
	EnableBalanceNotify   bool  `json:"enable_balance_notify"`
	EnableLoginNotify     bool  `json:"enable_login_notify"`
	EnableSubscribeNotify bool  `json:"enable_subscribe_notify"`
	EnableTradeNotify     bool  `json:"enable_trade_notify"`
}

type UpdateUserPasswordRequest struct {
	Password string `json:"password" validate:"required"`
}

type UpdateUserRulesRequest struct {
	Rules []string `json:"rules" validate:"required"`
}

type UpdateUserSubscribeNoteRequest struct {
	UserSubscribeId int64  `json:"user_subscribe_id" validate:"required"`
	Note            string `json:"note" validate:"max=500"`
}

type UpdateUserSubscribeRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id"`
	SubscribeId     int64 `json:"subscribe_id"`
	Traffic         int64 `json:"traffic"`
	ExpiredAt       int64 `json:"expired_at"`
	Upload          int64 `json:"upload"`
	Download        int64 `json:"download"`
}

type UpdateUserTicketStatusRequest struct {
	Id     int64  `json:"id" validate:"required"`
	Status *uint8 `json:"status" validate:"required"`
}

type User struct {
	Id                    int64            `json:"id"`
	Avatar                string           `json:"avatar"`
	Balance               int64            `json:"balance"`
	Commission            int64            `json:"commission"`
	ReferralPercentage    uint8            `json:"referral_percentage"`
	OnlyFirstPurchase     bool             `json:"only_first_purchase"`
	GiftAmount            int64            `json:"gift_amount"`
	Telegram              int64            `json:"telegram"`
	ReferCode             string           `json:"refer_code"`
	RefererId             int64            `json:"referer_id"`
	Enable                bool             `json:"enable"`
	IsAdmin               bool             `json:"is_admin,omitempty"`
	EnableBalanceNotify   bool             `json:"enable_balance_notify"`
	EnableLoginNotify     bool             `json:"enable_login_notify"`
	EnableSubscribeNotify bool             `json:"enable_subscribe_notify"`
	EnableTradeNotify     bool             `json:"enable_trade_notify"`
	AuthMethods           []UserAuthMethod `json:"auth_methods"`
	UserDevices           []UserDevice     `json:"user_devices"`
	Rules                 []string         `json:"rules"`
	CreatedAt             int64            `json:"created_at"`
	UpdatedAt             int64            `json:"updated_at"`
	DeletedAt             int64            `json:"deleted_at,omitempty"`
}

type UserAffiliate struct {
	Avatar       string `json:"avatar"`
	Identifier   string `json:"identifier"`
	RegisteredAt int64  `json:"registered_at"`
	Enable       bool   `json:"enable"`
}

type UserAuthMethod struct {
	AuthType       string `json:"auth_type"`
	AuthIdentifier string `json:"auth_identifier"`
	Verified       bool   `json:"verified"`
}

type UserDevice struct {
	Id         int64  `json:"id"`
	Ip         string `json:"ip"`
	Identifier string `json:"identifier"`
	UserAgent  string `json:"user_agent"`
	Online     bool   `json:"online"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type UserLoginLog struct {
	Id        int64  `json:"id"`
	UserId    int64  `json:"user_id"`
	LoginIP   string `json:"login_ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
	Timestamp int64  `json:"timestamp"`
}

type UserLoginRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email" validate:"required"`
	Password   string `json:"password" validate:"required"`
	IP         string `header:"X-Original-Forwarded-For"`
	UserAgent  string `header:"User-Agent"`
	LoginType  string `header:"Login-Type"`
	CfToken    string `json:"cf_token,optional"`
}

type UserRegisterRequest struct {
	Identifier string `json:"identifier"`
	Email      string `json:"email" validate:"required"`
	Password   string `json:"password" validate:"required"`
	Invite     string `json:"invite,optional"`
	Code       string `json:"code,optional"`
	IP         string `header:"X-Original-Forwarded-For"`
	UserAgent  string `header:"User-Agent"`
	LoginType  string `header:"Login-Type"`
	CfToken    string `json:"cf_token,optional"`
}

type UserStatistics struct {
	Date              string           `json:"date,omitempty"`
	Register          int64            `json:"register"`
	NewOrderUsers     int64            `json:"new_order_users"`
	RenewalOrderUsers int64            `json:"renewal_order_users"`
	List              []UserStatistics `json:"list,omitempty"`
}

type UserStatisticsResponse struct {
	Today   UserStatistics `json:"today"`
	Monthly UserStatistics `json:"monthly"`
	All     UserStatistics `json:"all"`
}

type UserSubscribe struct {
	Id          int64     `json:"id"`
	UserId      int64     `json:"user_id"`
	OrderId     int64     `json:"order_id"`
	SubscribeId int64     `json:"subscribe_id"`
	Subscribe   Subscribe `json:"subscribe"`
	StartTime   int64     `json:"start_time"`
	ExpireTime  int64     `json:"expire_time"`
	FinishedAt  int64     `json:"finished_at"`
	ResetTime   int64     `json:"reset_time"`
	Traffic     int64     `json:"traffic"`
	Download    int64     `json:"download"`
	Upload      int64     `json:"upload"`
	Token       string    `json:"token"`
	Status      uint8     `json:"status"`
	Short       string    `json:"short"`
	CreatedAt   int64     `json:"created_at"`
	UpdatedAt   int64     `json:"updated_at"`
}

type UserSubscribeDetail struct {
	Id          int64     `json:"id"`
	UserId      int64     `json:"user_id"`
	User        User      `json:"user"`
	OrderId     int64     `json:"order_id"`
	SubscribeId int64     `json:"subscribe_id"`
	Subscribe   Subscribe `json:"subscribe"`
	StartTime   int64     `json:"start_time"`
	ExpireTime  int64     `json:"expire_time"`
	ResetTime   int64     `json:"reset_time"`
	Traffic     int64     `json:"traffic"`
	Download    int64     `json:"download"`
	Upload      int64     `json:"upload"`
	Token       string    `json:"token"`
	Status      uint8     `json:"status"`
	CreatedAt   int64     `json:"created_at"`
	UpdatedAt   int64     `json:"updated_at"`
}

type UserSubscribeInfo struct {
	Id          int64                    `json:"id"`
	UserId      int64                    `json:"user_id"`
	OrderId     int64                    `json:"order_id"`
	SubscribeId int64                    `json:"subscribe_id"`
	StartTime   int64                    `json:"start_time"`
	ExpireTime  int64                    `json:"expire_time"`
	FinishedAt  int64                    `json:"finished_at"`
	ResetTime   int64                    `json:"reset_time"`
	Traffic     int64                    `json:"traffic"`
	Download    int64                    `json:"download"`
	Upload      int64                    `json:"upload"`
	Token       string                   `json:"token"`
	Status      uint8                    `json:"status"`
	CreatedAt   int64                    `json:"created_at"`
	UpdatedAt   int64                    `json:"updated_at"`
	IsTryOut    bool                     `json:"is_try_out"`
	Nodes       []*UserSubscribeNodeInfo `json:"nodes"`
}

type UserSubscribeLog struct {
	Id              int64  `json:"id"`
	UserId          int64  `json:"user_id"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
	Token           string `json:"token"`
	IP              string `json:"ip"`
	UserAgent       string `json:"user_agent"`
	Timestamp       int64  `json:"timestamp"`
}

type UserSubscribeNodeInfo struct {
	Id        int64    `json:"id"`
	Name      string   `json:"name"`
	Uuid      string   `json:"uuid"`
	Protocol  string   `json:"protocol"`
	Port      uint16   `json:"port"`
	Address   string   `json:"address"`
	Tags      []string `json:"tags"`
	Country   string   `json:"country"`
	City      string   `json:"city"`
	CreatedAt int64    `json:"created_at"`
}

type UserSubscribeTrafficLog struct {
	SubscribeId int64  `json:"subscribe_id"` // Subscribe ID
	UserId      int64  `json:"user_id"`      // User ID
	Upload      int64  `json:"upload"`       // Upload traffic in bytes
	Download    int64  `json:"download"`     // Download traffic in bytes
	Total       int64  `json:"total"`        // Total traffic in bytes (Upload + Download)
	Date        string `json:"date"`         // Date in YYYY-MM-DD format
	Details     bool   `json:"details"`      // Whether to show detailed traffic
}

type UserTraffic struct {
	SID      int64 `json:"uid"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}

type UserTrafficData struct {
	SID      int64 `json:"sid"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}

type VeifyConfig struct {
	TurnstileSiteKey          string `json:"turnstile_site_key"`
	EnableLoginVerify         bool   `json:"enable_login_verify"`
	EnableRegisterVerify      bool   `json:"enable_register_verify"`
	EnableResetPasswordVerify bool   `json:"enable_reset_password_verify"`
}

type VerifyCodeConfig struct {
	VerifyCodeExpireTime int64 `json:"verify_code_expire_time"`
	VerifyCodeLimit      int64 `json:"verify_code_limit"`
	VerifyCodeInterval   int64 `json:"verify_code_interval"`
}

type VerifyConfig struct {
	TurnstileSiteKey          string `json:"turnstile_site_key"`
	TurnstileSecret           string `json:"turnstile_secret"`
	EnableLoginVerify         bool   `json:"enable_login_verify"`
	EnableRegisterVerify      bool   `json:"enable_register_verify"`
	EnableResetPasswordVerify bool   `json:"enable_reset_password_verify"`
}

type VerifyEmailRequest struct {
	Email string `json:"email" validate:"required"`
	Code  string `json:"code" validate:"required"`
}

type VersionResponse struct {
	Version string `json:"version"`
}

type Vless struct {
	Port            int             `json:"port" validate:"required"`
	Flow            string          `json:"flow" validate:"required"`
	Transport       string          `json:"transport" validate:"required"`
	TransportConfig TransportConfig `json:"transport_config"`
	Security        string          `json:"security" validate:"required"`
	SecurityConfig  SecurityConfig  `json:"security_config"`
}

type VlessProtocol struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Network        string `json:"network"`
	Transport      string `json:"transport"`
	Security       string `json:"security"`
	SecurityConfig string `json:"security_config"`
	XTLS           string `json:"xtls"`
}

type Vmess struct {
	Port            int             `json:"port" validate:"required"`
	Transport       string          `json:"transport" validate:"required"`
	TransportConfig TransportConfig `json:"transport_config"`
	Security        string          `json:"security" validate:"required"`
	SecurityConfig  SecurityConfig  `json:"security_config"`
}

type VmessProtocol struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	EnableTLS *bool  `json:"enable_tls"`
	TLSConfig string `json:"tls_config"`
	Network   string `json:"network"`
	Transport string `json:"transport"`
}

type WithdrawalLog struct {
	Id        int64  `json:"id"`
	UserId    int64  `json:"user_id"`
	Amount    int64  `json:"amount"`
	Content   string `json:"content"`
	Status    uint8  `json:"status"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}
