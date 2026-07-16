package oauthstate

import (
	"context"

	"github.com/redis/go-redis/v9"
)

var consumeScript = redis.NewScript(`
local value = redis.call("GET", KEYS[1])
if not value then
  return false
end
redis.call("DEL", KEYS[1])
return value
`)

// Consume atomically returns and deletes an OAuth state value. The Lua
// implementation keeps compatibility with Redis versions older than 6.2.
func Consume(ctx context.Context, client *redis.Client, key string) (string, error) {
	return consumeScript.Run(ctx, client, []string{key}).Text()
}
