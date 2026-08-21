package dto

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

type CreateBatchSendEmailTaskRequest struct {
	Subject           string `json:"subject" validate:"required"`
	Content           string `json:"content" validate:"required"`
	Scope             int8   `json:"scope" validate:"required,oneof=1 2 3 4 5"`
	RegisterStartTime int64  `json:"register_start_time,omitempty" validate:"gte=0"`
	RegisterEndTime   int64  `json:"register_end_time,omitempty" validate:"gte=0"`
	Additional        string `json:"additional,omitempty"`
	Scheduled         int64  `json:"scheduled,omitempty" validate:"gte=0"`
	Interval          uint8  `json:"interval,omitempty"`
	Limit             uint64 `json:"limit,omitempty"`
}

type CreateQuotaTaskRequest struct {
	Subscribers  []int64 `json:"subscribers" validate:"dive,gt=0"`
	IsActive     *bool   `json:"is_active"`
	StartTime    int64   `json:"start_time" validate:"gte=0"`
	EndTime      int64   `json:"end_time" validate:"gte=0"`
	ResetTraffic bool    `json:"reset_traffic"`
	Days         uint64  `json:"days"`
	GiftType     uint8   `json:"gift_type" validate:"oneof=0 1 2"`
	GiftValue    uint64  `json:"gift_value"`
}

type GetBatchSendEmailTaskListRequest struct {
	Page   int    `form:"page" validate:"omitempty,gt=0"`
	Size   int    `form:"size" validate:"omitempty,gt=0,lte=100"`
	Scope  *int8  `form:"scope,omitempty" validate:"omitempty,oneof=1 2 3 4 5"`
	Status *uint8 `form:"status,omitempty" validate:"omitempty,oneof=0 1 2 3 4 5"`
}

type GetBatchSendEmailTaskListResponse struct {
	Total int64                `json:"total"`
	List  []BatchSendEmailTask `json:"list"`
}

type GetBatchSendEmailTaskStatusRequest struct {
	Id int64 `json:"id" validate:"required,gt=0"`
}

type GetBatchSendEmailTaskStatusResponse struct {
	Status  uint8  `json:"status"`
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Errors  string `json:"errors"`
}

type GetPreSendEmailCountRequest struct {
	Scope             int8   `json:"scope" validate:"required,oneof=1 2 3 4 5"`
	RegisterStartTime int64  `json:"register_start_time,omitempty"`
	RegisterEndTime   int64  `json:"register_end_time,omitempty"`
	Additional        string `json:"additional,omitempty"`
}

type GetPreSendEmailCountResponse struct {
	Count int64 `json:"count"`
}

type QueryQuotaTaskListRequest struct {
	Page   int    `form:"page" validate:"omitempty,gt=0"`
	Size   int    `form:"size" validate:"omitempty,gt=0,lte=100"`
	Status *uint8 `form:"status,omitempty" validate:"omitempty,oneof=0 1 2 3 4 5"`
}

type QueryQuotaTaskListResponse struct {
	Total int64       `json:"total"`
	List  []QuotaTask `json:"list"`
}

type QueryQuotaTaskPreCountRequest struct {
	Subscribers []int64 `json:"subscribers"`
	IsActive    *bool   `json:"is_active"`
	StartTime   int64   `json:"start_time" validate:"gte=0"`
	EndTime     int64   `json:"end_time" validate:"gte=0"`
}

type QueryQuotaTaskPreCountResponse struct {
	Count int64 `json:"count"`
}

type QueryQuotaTaskStatusRequest struct {
	Id int64 `json:"id" validate:"required,gt=0"`
}

type QueryQuotaTaskStatusResponse struct {
	Status  uint8  `json:"status"`
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Errors  string `json:"errors"`
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

type StopBatchSendEmailTaskRequest struct {
	Id int64 `json:"id" validate:"required,gt=0"`
}
