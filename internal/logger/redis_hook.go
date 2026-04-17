package logger

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type redisHook struct{}

// NewRedisHook returns a redis.Hook that logs every command and pipeline with full args and duration.
// In non-debug builds the hot path skips all string formatting — only errors are logged.
func NewRedisHook() redis.Hook { return &redisHook{} }

func (h *redisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *redisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		isDebug := L.Core().Enabled(zapcore.DebugLevel)

		var start time.Time
		if isDebug {
			start = time.Now()
		}

		err := next(ctx, cmd)

		if isDebug {
			dur := time.Since(start)
			fields := []zap.Field{
				zap.String("cmd", cmd.FullName()),
				zap.String("args", fmt.Sprintf("%v", cmd.Args())),
				zap.Duration("duration", dur),
			}
			if err != nil && err != redis.Nil {
				fields = append(fields, zap.Error(err))
				L.Error("[redis] command", fields...)
			} else {
				L.Debug("[redis] command", fields...)
			}
		} else if err != nil && err != redis.Nil {
			L.Error("[redis] command failed",
				zap.String("cmd", cmd.FullName()),
				zap.Error(err),
			)
		}
		return err
	}
}

func (h *redisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		isDebug := L.Core().Enabled(zapcore.DebugLevel)

		var start time.Time
		if isDebug {
			start = time.Now()
		}

		err := next(ctx, cmds)

		if isDebug {
			dur := time.Since(start)
			names := make([]string, len(cmds))
			args := make([]string, len(cmds))
			for i, c := range cmds {
				names[i] = c.FullName()
				args[i] = fmt.Sprintf("%v", c.Args())
			}
			fields := []zap.Field{
				zap.Int("count", len(cmds)),
				zap.String("cmds", strings.Join(names, " | ")),
				zap.String("args", strings.Join(args, " | ")),
				zap.Duration("duration", dur),
			}
			if err != nil && err != redis.Nil {
				fields = append(fields, zap.Error(err))
				L.Error("[redis] pipeline", fields...)
			} else {
				L.Debug("[redis] pipeline", fields...)
			}
		} else if err != nil && err != redis.Nil {
			names := make([]string, len(cmds))
			for i, c := range cmds {
				names[i] = c.FullName()
			}
			L.Error("[redis] pipeline failed",
				zap.String("cmds", strings.Join(names, " | ")),
				zap.Error(err),
			)
		}
		return err
	}
}
