package dto

type GetStatResponse struct {
	User     int64    `json:"user"`
	Node     int64    `json:"node"`
	Country  int64    `json:"country"`
	Protocol []string `json:"protocol"`
}

type RevenueStatisticsResponse struct {
	Today   OrdersStatistics `json:"today"`
	Monthly OrdersStatistics `json:"monthly"`
	All     OrdersStatistics `json:"all"`
}

type TimePeriod struct {
	StartTime  string  `json:"start_time"`
	EndTime    string  `json:"end_time"`
	Multiplier float32 `json:"multiplier"`
}
