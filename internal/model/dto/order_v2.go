package dto

// V2GuestOrderRequest contains the account information held until a guest
// purchase is activated. It is only accepted for anonymous purchase orders.
type V2GuestOrderRequest struct {
	AuthType   string `json:"auth_type"`
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code,omitempty"`
}

type V2CreateOrderRequest struct {
	Type            string               `json:"type"`
	PaymentID       int64                `json:"payment_id"`
	SubscribeID     int64                `json:"subscribe_id,omitempty"`
	UserSubscribeID int64                `json:"user_subscribe_id,omitempty"`
	Quantity        int64                `json:"quantity,omitempty"`
	Coupon          string               `json:"coupon,omitempty"`
	Amount          int64                `json:"amount,omitempty"`
	ReturnURL       string               `json:"return_url,omitempty"`
	Guest           *V2GuestOrderRequest `json:"guest,omitempty"`
}

type V2CheckoutOrderRequest struct {
	CheckoutToken string `json:"checkout_token,omitempty"`
	ReturnURL     string `json:"return_url,omitempty"`
}

type V2EventTicketRequest struct {
	CheckoutToken string `json:"checkout_token,omitempty"`
}

// V2OrderSessionRequest exchanges a guest checkout capability for a normal
// user session after the corresponding purchase has created its account.
// It deliberately reuses the capability rather than putting a bearer token in
// an SSE URL or event payload.
type V2OrderSessionRequest struct {
	CheckoutToken string `json:"checkout_token,omitempty"`
}

type V2OrderResponse struct {
	Order         V2OrderSnapshot `json:"order"`
	Payment       *V2OrderPayment `json:"payment,omitempty"`
	Events        V2OrderEvents   `json:"events"`
	CheckoutToken string          `json:"checkout_token,omitempty"`
}

type V2OrderSnapshot struct {
	OrderNo           string `json:"order_no"`
	Status            string `json:"status"`
	PaymentStatus     string `json:"payment_status"`
	FulfillmentStatus string `json:"fulfillment_status"`
	StateVersion      int64  `json:"state_version"`
	Amount            int64  `json:"amount"`
	Currency          string `json:"currency"`
	ExpiresAt         int64  `json:"expires_at"`
}

type V2OrderPayment struct {
	Type          string         `json:"type"`
	CheckoutURL   string         `json:"checkout_url,omitempty"`
	Stripe        *StripePayment `json:"stripe,omitempty"`
	PaymentStatus string         `json:"payment_status"`
}

type V2OrderEvents struct {
	URL             string `json:"url"`
	TicketExpiresAt int64  `json:"ticket_expires_at"`
}

type V2EventTicketResponse struct {
	URL             string `json:"url"`
	TicketExpiresAt int64  `json:"ticket_expires_at"`
}

type V2OrderSessionResponse struct {
	AccessToken string `json:"access_token"`
}
