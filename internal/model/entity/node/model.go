package node

import "time"

const (
	// ServerCacheTTL TTL for node hot-path server caches (server config and user list)
	ServerCacheTTL = 5 * time.Minute

	// ServerUserListCacheKey Server User List Cache Key
	ServerUserListCacheKey = "server:user:"

	// ServerConfigCacheKey Server Config Cache Key
	ServerConfigCacheKey = "server:config:"

	// ServerCacheIndexKey tracks the exact response-cache keys generated for a
	// server so invalidation does not need to scan the entire Redis keyspace.
	ServerCacheIndexKey = "server:cache:index:%d"

	// ServerCacheGenerationKey fences response-cache fills that started before
	// a server configuration mutation completed.
	ServerCacheGenerationKey = "server:cache:generation:%d"
)

// FilterParams Filter Server Params
type FilterParams struct {
	Page   int
	Size   int
	Ids    []int64 // Server IDs
	Search string
}

type FilterNodeParams struct {
	Page     int      // Page Number
	Size     int      // Page Size
	NodeId   []int64  // Node IDs
	ServerId []int64  // Server IDs
	Tag      []string // Tags
	Search   string   // Search Address or Name
	Protocol string   // Protocol
	Preload  bool     // Preload Server
	Enabled  *bool    // Enabled
}

type SortItem struct {
	Id   int64
	Sort int64
}
