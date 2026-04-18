package logger

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type redisHook struct{}

// NewRedisHook returns a redis.Hook (no-op: logging removed).
func NewRedisHook() redis.Hook { return &redisHook{} }

func (h *redisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *redisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		return next(ctx, cmd)
	}
}

func (h *redisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		return next(ctx, cmds)
	}
}
