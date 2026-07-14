package node

import (
	"encoding/json"
	"time"
)

type (
	Status struct {
		Cpu       float64 `json:"cpu"`
		Mem       float64 `json:"mem"`
		Disk      float64 `json:"disk"`
		UpdatedAt int64   `json:"updated_at"`
	}

	OnlineUserSubscribe map[int64][]string
)

// Marshal  to json string
func (s *Status) Marshal() string {
	type Alias Status
	data, _ := json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
	return string(data)
}

// Unmarshal from json string
func (s *Status) Unmarshal(data string) error {
	type Alias Status
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	return json.Unmarshal([]byte(data), &aux)
}

const (
	Expiry                                = 300 * time.Second              // Cache expiry time in seconds
	StatusCacheKey                        = "node:status:%d"               // Node status cache key format (Server ID and protocol) Example: node:status:1:shadowsocks
	OnlineUserCacheKeyWithSubscribe       = "node:online:subscribe:%d:%s"  // Online user subscribe cache key format (Server ID and protocol) Example: node:online:subscribe:1:shadowsocks
	OnlineUserSubscribeCacheKeyWithGlobal = "node:online:subscribe:global" // Online user global subscribe cache key
)
