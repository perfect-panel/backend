package svc

import (
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/config"
)

func redisOpt(c config.Config) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{Addr: c.Redis.Host, Password: c.Redis.Pass, DB: 5}
}

func NewAsynqClient(c config.Config) *asynq.Client {
	return asynq.NewClient(redisOpt(c))
}

func NewAsynqInspector(c config.Config) *asynq.Inspector {
	return asynq.NewInspector(redisOpt(c))
}
