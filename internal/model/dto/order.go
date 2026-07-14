package dto

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

type PurchaseOrderRequest struct {
	SubscribeId int64  `json:"subscribe_id"`
	Quantity    int64  `json:"quantity" validate:"required,gt=0,lte=1000"`
	Payment     int64  `json:"payment,omitempty"`
	Coupon      string `json:"coupon,omitempty"`
}

type PurchaseOrderResponse struct {
	OrderNo string `json:"order_no"`
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

type RechargeOrderRequest struct {
	Amount  int64 `json:"amount" validate:"required,gt=0,lte=2000000000"`
	Payment int64 `json:"payment"`
}

type RechargeOrderResponse struct {
	OrderNo string `json:"order_no"`
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

type ResetTrafficOrderRequest struct {
	UserSubscribeID int64 `json:"user_subscribe_id"`
	Payment         int64 `json:"payment"`
}

type ResetTrafficOrderResponse struct {
	OrderNo string `json:"order_no"`
}

type UpdateOrderStatusRequest struct {
	Id        int64  `json:"id" validate:"required"`
	Status    uint8  `json:"status" validate:"required"`
	PaymentId int64  `json:"payment_id,omitempty"`
	TradeNo   string `json:"trade_no,omitempty"`
}
