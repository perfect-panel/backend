package dto

type BatchDeleteCouponRequest struct {
	Ids []int64 `json:"ids" validate:"required"`
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

type DeleteCouponRequest struct {
	Id int64 `json:"id" validate:"required"`
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

type SubscribeDiscount struct {
	Quantity int64   `json:"quantity"`
	Discount float64 `json:"discount"`
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
