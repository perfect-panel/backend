package traffic

import "time"

//goland:noinspection GoNameStartsWithPackageName
type TrafficLog struct {
	Id          int64     `gorm:"primaryKey"`
	ServerId    int64     `gorm:"index:idx_server_id;not null;comment:Server ID"`
	UserId      int64     `gorm:"index:idx_user_id;not null;comment:User ID"`
	SubscribeId int64     `gorm:"index:idx_subscribe_id;not null;comment:Subscription ID"`
	Download    int64     `gorm:"default:0;comment:Download Traffic"`
	Upload      int64     `gorm:"default:0;comment:Upload Traffic"`
	Timestamp   time.Time `gorm:"default:CURRENT_TIMESTAMP(3);not null;comment:Traffic Log Time"`
}

type TotalTraffic struct {
	Download int64
	Upload   int64
}

type ServerTrafficRanking struct {
	ServerId int64
	Download int64
	Upload   int64
	Total    int64
}

type UserTrafficRanking struct {
	UserId      int64
	SubscribeId int64
	Download    int64
	Upload      int64
	Total       int64
}

func (TrafficLog) TableName() string {
	return "traffic_log"
}

// TrafficLogDetailsFilter traffic 明细查询过滤条件
type TrafficLogDetailsFilter struct {
	ServerId    int64
	UserId      int64
	SubscribeId int64
	Start       time.Time
	End         time.Time
	Page        int
	Size        int
}
