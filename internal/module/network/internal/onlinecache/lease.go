// Package onlinecache shares the lease update used by global device counts
// and per-server online reports.
package onlinecache

import "github.com/redis/go-redis/v9"

// RenewScript accepts a TTL in seconds followed by the members to renew.
// Compare scores in Lua rather than ZADD GT to retain Redis 6.0 support.
// Use Eval when queuing this in a pipeline: Run cannot retry NOSCRIPT after Exec.
var RenewScript = redis.NewScript(`
redis.replicate_commands()
local now = tonumber(redis.call('TIME')[1])
local ttl = tonumber(ARGV[1])
local expires = now + ttl
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
for i = 2, #ARGV do
  local previous = tonumber(redis.call('ZSCORE', KEYS[1], ARGV[i])) or 0
  if previous < expires then
    redis.call('ZADD', KEYS[1], expires, ARGV[i])
  end
end
redis.call('EXPIRE', KEYS[1], ttl)
return 1
`)
