package dto

type ServerPushUserTrafficRequest struct {
	ServerCommon
	ReportID string        `json:"-" swaggerignore:"true"`
	Traffic  []UserTraffic `json:"traffic"`
}

// UserTraffic is one node-reported usage entry: SID is the user_subscribe id
// GET /v1/server/user handed the node, Upload and Download the bytes since
// the previous report. The traffic pipeline drops (and logs) entries that are
// negative, exceed trafficagg.MaxReportedTraffic or name a subscription the
// reporting server does not serve; the rest of the report still applies.
type UserTraffic struct {
	SID      int64 `json:"uid"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}
