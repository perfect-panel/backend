package config

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/perfect-panel/backend/internal/infra/rediskeys"
	"github.com/redis/go-redis/v9"
)

// WithURI applies a first-install connection URI to the file's Redis settings.
// QueueDB and a prefix supplied in the file survive a URI without ?prefix=.
func (c RedisConfig) WithURI(uri string) (RedisConfig, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "redis" || u.Hostname() == "" {
		return c, fmt.Errorf("invalid Redis URI")
	}
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	db := 0
	if path := strings.TrimPrefix(u.Path, "/"); path != "" {
		db, err = strconv.Atoi(path)
		if err != nil || db < 0 {
			return c, fmt.Errorf("invalid Redis database index")
		}
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return c, fmt.Errorf("invalid Redis URI query")
	}
	c.Host = net.JoinHostPort(u.Hostname(), port)
	c.Username = u.User.Username()
	c.Pass, _ = u.User.Password()
	c.DB = db
	if q.Has("prefix") {
		c.KeyPrefix = q.Get("prefix")
	}
	return c, c.Validate()
}

// Validate checks the hash-tag constraint of Asynq's Redis key builder.
func (c RedisConfig) Validate() error {
	if i := strings.IndexByte(c.KeyPrefix, '{'); i >= 0 && strings.HasPrefix(c.KeyPrefix[i:], "{}") {
		return fmt.Errorf("Redis.KeyPrefix cannot start its first hash tag with {}")
	}
	return nil
}

// NewClient builds the application Redis connection. Queue clients select
// QueueDB and let Asynq apply the same project prefix to its own keyspace.
func (c RedisConfig) NewClient() *redis.Client {
	client := redis.NewClient(&redis.Options{Addr: c.Host, Username: c.Username, Password: c.Pass, DB: c.DB})
	rediskeys.New(c.KeyPrefix).Install(client)
	return client
}

func (c RedisConfig) Ping(ctx context.Context) error {
	client := c.NewClient()
	defer func() { _ = client.Close() }()
	return client.Ping(ctx).Err()
}
