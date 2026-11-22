package dto

import (
	"github.com/perfect-panel/backend/internal/infra/integration"
)

// Cross-domain view types are module-owned snapshots. They deliberately
// duplicate JSON shapes instead of coupling one module's API to another.
type GetLoginLogRequest struct {
	Page int `form:"page" validate:"required,gt=0"`
	Size int `form:"size" validate:"required,gt=0,lte=100"`
}

type GetLoginLogResponse struct {
	List  []UserLoginLog `json:"list"`
	Total int64          `json:"total"`
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

type AuthPlatformInfoSnapshot = integration.Info // @name dto.PlatformInfo

type AuthPlatformResponse struct {
	List []AuthPlatformInfoSnapshot `json:"list"`
} // @name dto.PlatformResponse

type UserLoginLog struct {
	Id               int64  `json:"id"`
	UserId           int64  `json:"user_id"`
	LoginIP          string `json:"login_ip"`
	UserAgent        string `json:"user_agent"`
	Success          bool   `json:"success"`
	Timestamp        int64  `json:"timestamp"`
	ActorID          int64  `json:"actor_id,omitempty"`
	IPCountryCode    string `json:"ip_country_code,omitempty"`
	IPCountry        string `json:"ip_country,omitempty"`
	IPRegion         string `json:"ip_region,omitempty"`
	IPCity           string `json:"ip_city,omitempty"`
	IPASN            uint   `json:"ip_asn,omitempty"`
	IPASOrganization string `json:"ip_as_organization,omitempty"`
}
