package dto

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

type CreateNodeRequest struct {
	Name     string   `json:"name"`
	Tags     []string `json:"tags,omitempty"`
	Port     uint16   `json:"port"`
	Address  string   `json:"address"`
	ServerId int64    `json:"server_id"`
	Protocol string   `json:"protocol"`
	Enabled  *bool    `json:"enabled"`
}

type DeleteNodeRequest struct {
	Id int64 `json:"id"`
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

type GetNodeMultiplierResponse struct {
	Periods []TimePeriod `json:"periods"`
}

type HasMigrateSeverNodeResponse struct {
	HasMigrate bool `json:"has_migrate"`
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

type NodeRelay struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Prefix string `json:"prefix"`
}

type PreViewNodeMultiplierResponse struct {
	CurrentTime string  `json:"current_time"`
	Ratio       float32 `json:"ratio"`
}

type QueryNodeTagResponse struct {
	Tags []string `json:"tags"`
}

type QueryUserSubscribeNodeListResponse struct {
	List []UserSubscribeInfo `json:"list"`
}

type ResetSortRequest struct {
	Sort []SortItem `json:"sort"`
}

type SetNodeMultiplierRequest struct {
	Periods []TimePeriod `json:"periods"`
}

type SortItem struct {
	Id   int64 `json:"id" validate:"required"`
	Sort int64 `json:"sort" validate:"required"`
}

type SubscribeSortRequest struct {
	Sort []SortItem `json:"sort"`
}

type ToggleNodeStatusRequest struct {
	Id     int64 `json:"id"`
	Enable *bool `json:"enable"`
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
