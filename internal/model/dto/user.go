package dto

type BatchDeleteUserRequest struct {
	Ids []int64 `json:"ids" validate:"required"`
}

type CommissionWithdrawRequest struct {
	Amount  int64  `json:"amount" validate:"required,gt=0,lte=2000000000"`
	Content string `json:"content"`
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

type DeleteUserAuthMethodRequest struct {
	UserId   int64  `json:"user_id"`
	AuthType string `json:"auth_type"`
}

type DeleteUserDeivceRequest struct {
	Id int64 `json:"id"`
}

type DeleteUserSubscribeRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id"`
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

type KickOfflineRequest struct {
	Id int64 `json:"id"`
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

type QueryUserSubscribeListResponse struct {
	List  []UserSubscribe `json:"list"`
	Total int64           `json:"total"`
}

type ResetUserSubscribeTokenRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id"`
}

type ToggleUserSubscribeStatusRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id"`
}

type UnbindDeviceRequest struct {
	Id int64 `json:"id" validate:"required"`
}

type UpdateBindEmailRequest struct {
	Email string `json:"email" validate:"required,email"`
}

type UpdateBindMobileRequest struct {
	AreaCode string `json:"area_code" validate:"required"`
	Mobile   string `json:"mobile" validate:"required"`
	Code     string `json:"code" validate:"required"`
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
	Password string `json:"password" validate:"required,min=8,max=128"`
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
