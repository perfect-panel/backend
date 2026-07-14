package dto

type BalanceLog struct {
	Type      uint16 `json:"type"`
	UserId    int64  `json:"user_id"`
	Amount    int64  `json:"amount"`
	OrderNo   string `json:"order_no,omitempty"`
	Balance   int64  `json:"balance"`
	Timestamp int64  `json:"timestamp"`
}

type CommissionLog struct {
	Type      uint16 `json:"type"`
	UserId    int64  `json:"user_id"`
	Amount    int64  `json:"amount"`
	OrderNo   string `json:"order_no"`
	Timestamp int64  `json:"timestamp"`
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

type FilterSubscribeLogRequest struct {
	FilterLogParams
	UserId          int64 `form:"user_id,optional"`
	UserSubscribeId int64 `form:"user_subscribe_id,optional"`
}

type FilterSubscribeLogResponse struct {
	Total int64          `json:"total"`
	List  []SubscribeLog `json:"list"`
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

type GetUserLoginLogsRequest struct {
	Page   int   `form:"page" validate:"required,gt=0"`
	Size   int   `form:"size" validate:"required,gt=0,lte=100"`
	UserId int64 `form:"user_id"`
}

type GetUserLoginLogsResponse struct {
	List  []UserLoginLog `json:"list"`
	Total int64          `json:"total"`
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

type QueryWithdrawalLogListRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type QueryWithdrawalLogListResponse struct {
	List  []WithdrawalLog `json:"list"`
	Total int64           `json:"total"`
}

type RegisterLog struct {
	UserId     int64  `json:"user_id"`
	AuthMethod string `json:"auth_method"`
	Identifier string `json:"identifier"`
	RegisterIP string `json:"register_ip"`
	UserAgent  string `json:"user_agent"`
	Timestamp  int64  `json:"timestamp"`
}

type ResetSubscribeLog struct {
	Type            uint16 `json:"type"`
	UserId          int64  `json:"user_id"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
	OrderNo         string `json:"order_no,omitempty"`
	Timestamp       int64  `json:"timestamp"`
}

type UserLoginLog struct {
	Id        int64  `json:"id"`
	UserId    int64  `json:"user_id"`
	LoginIP   string `json:"login_ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
	Timestamp int64  `json:"timestamp"`
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
