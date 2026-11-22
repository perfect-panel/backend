// Package rediskeys scopes PPanel's application Redis commands to one project.
// Asynq handles its own key and channel names using the same prefix.
package rediskeys

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Prefix includes the separator. Match Asynq's handling of a trailing colon.
type Prefix string

func New(prefix string) Prefix {
	if prefix == "" {
		return ""
	}
	return Prefix(strings.TrimRight(prefix, ":") + ":")
}

func (p Prefix) Key(key string) string { return string(p) + key }

// Argument marks a logical key passed through Lua ARGV rather than KEYS.
// Without a prefix hook go-redis writes its original bytes.
type Argument string

func (k Argument) MarshalBinary() ([]byte, error) { return []byte(k), nil }

// Install applies a prefix to business commands, including Lua and pipelines.
// Subscriptions use a dedicated connection and construct channel names with Key.
func (p Prefix) Install(client *redis.Client) {
	if p != "" {
		client.AddHook(p)
	}
}

func (p Prefix) DialHook(next redis.DialHook) redis.DialHook { return next }

func (p Prefix) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := slices.Clone(cmd.Args())
		defer copy(cmd.Args(), args)
		if err := p.scope(cmd); err != nil {
			return err
		}
		return next(ctx, cmd)
	}
}

func (p Prefix) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		// The returned Cmds and SCAN iterators retain their logical arguments.
		original := make([][]any, len(cmds))
		defer func() {
			for i, args := range original {
				copy(cmds[i].Args(), args)
			}
		}()
		for i, cmd := range cmds {
			original[i] = slices.Clone(cmd.Args())
			if err := p.scope(cmd); err != nil {
				for _, c := range cmds {
					c.SetErr(err)
				}
				return err
			}
		}
		return next(ctx, cmds)
	}
}

var globEscaper = strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`, "]", `\]`)

func (p Prefix) scope(cmd redis.Cmder) error {
	args := cmd.Args()
	var keys []any
	switch cmd.Name() {
	case "auth", "hello", "client", "select", "ping", "time", "multi", "exec", "discard", "quit":
		return nil
	case "publish", "get", "getdel", "set", "setnx", "setex", "psetex", "incr", "decr", "expire", "pexpire", "ttl", "pttl",
		"hget", "hgetall", "hset", "hdel", "hincrby", "hlen", "smembers",
		"zadd", "zcard", "zcount", "zrange", "zrangebyscore", "zrem", "zremrangebyscore":
		if len(args) < 2 {
			return fmt.Errorf("missing key for Redis command %q", cmd.Name())
		}
		keys = args[1:2]
	case "del", "exists", "mget", "renamenx":
		keys = args[1:]
	case "eval", "evalsha", "eval_ro", "evalsha_ro":
		if len(args) < 3 {
			return fmt.Errorf("missing Redis script key count")
		}
		n, ok := args[2].(int)
		if !ok || n < 0 || n > len(args)-3 {
			return fmt.Errorf("invalid Redis script key count")
		}
		keys = args[3 : 3+n]
		for i := 3 + n; i < len(args); i++ {
			if k, ok := args[i].(Argument); ok {
				args[i] = p.Key(string(k))
			}
		}
	case "scan":
		for i := 2; i+1 < len(args); i += 2 {
			if strings.EqualFold(fmt.Sprint(args[i]), "match") {
				args[i+1] = globEscaper.Replace(string(p)) + fmt.Sprint(args[i+1])
				return nil
			}
		}
		return fmt.Errorf("project-scoped SCAN requires a MATCH pattern")
	default:
		return fmt.Errorf("unsupported project-scoped Redis command %q", cmd.Name())
	}
	for i, arg := range keys {
		key, ok := arg.(string)
		if !ok {
			return fmt.Errorf("invalid key in Redis command %q", cmd.Name())
		}
		keys[i] = p.Key(key)
	}
	return nil
}
