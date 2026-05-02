// Package middleware provides service decorators for the groundstation service.
package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/absmach/zenith-link/groundstation"
	"github.com/absmach/zenith-link/pkg/zenith"
)

var _ groundstation.Service = (*loggingMiddleware)(nil)

type loggingMiddleware struct {
	logger *slog.Logger
	svc    groundstation.Service
}

func NewLogging(svc groundstation.Service, logger *slog.Logger) groundstation.Service {
	return &loggingMiddleware{logger: logger, svc: svc}
}

func (lm *loggingMiddleware) Receive(ctx context.Context, rawFrame []byte) (tm zenith.Telemetry, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Int("frame_bytes", len(rawFrame)),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("groundstation.Receive", args...)
			return
		}
		args = append(args, slog.Uint64("sequence", uint64(tm.Sequence)))
		lm.logger.Info("groundstation.Receive", args...)
	}(time.Now())
	return lm.svc.Receive(ctx, rawFrame)
}

func (lm *loggingMiddleware) Latest(ctx context.Context) (st groundstation.LatestState, err error) {
	defer func(begin time.Time) {
		args := []any{slog.String("duration", time.Since(begin).String())}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("groundstation.Latest", args...)
			return
		}
		lm.logger.Debug("groundstation.Latest", args...)
	}(time.Now())
	return lm.svc.Latest(ctx)
}

func (lm *loggingMiddleware) Subscribe(ctx context.Context) (ch <-chan zenith.Telemetry, err error) {
	defer func(begin time.Time) {
		args := []any{slog.String("duration", time.Since(begin).String())}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("groundstation.Subscribe", args...)
			return
		}
		lm.logger.Debug("groundstation.Subscribe", args...)
	}(time.Now())
	return lm.svc.Subscribe(ctx)
}
