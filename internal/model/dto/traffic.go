package dto

type FilterServerTrafficLogRequest struct {
	FilterLogParams
	ServerId int64 `form:"server_id,optional"`
}

type FilterServerTrafficLogResponse struct {
	Total int64              `json:"total"`
	List  []ServerTrafficLog `json:"list"`
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

type ResetSubscribeTrafficLog struct {
	Id              int64  `json:"id"`
	Type            uint16 `json:"type"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
	OrderNo         string `json:"order_no,omitempty"`
	Timestamp       int64  `json:"timestamp"`
}

type ResetUserSubscribeTrafficRequest struct {
	UserSubscribeId int64 `json:"user_subscribe_id"`
}

type ServerPushUserTrafficRequest struct {
	ServerCommon
	Traffic []UserTraffic `json:"traffic"`
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
