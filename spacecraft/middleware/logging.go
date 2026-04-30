// Package middleware provides service decorators for the spacecraft service.
package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/absmach/zenith-link/spacecraft"
	"github.com/absmach/zenith-link/spacecraft/inference"
)

var _ spacecraft.Service = (*loggingMiddleware)(nil)

type loggingMiddleware struct {
	logger *slog.Logger
	svc    spacecraft.Service
}

// NewLogging wraps svc with structured JSON logging on every method call.
func NewLogging(svc spacecraft.Service, logger *slog.Logger) spacecraft.Service {
	return &loggingMiddleware{logger: logger, svc: svc}
}

func (lm *loggingMiddleware) Telemetry(ctx context.Context, t time.Time) (tm zenith.Telemetry, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Time("query_time", t),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("spacecraft.Telemetry", args...)
			return
		}
		lm.logger.Debug("spacecraft.Telemetry", args...)
	}(time.Now())
	return lm.svc.Telemetry(ctx, t)
}

func (lm *loggingMiddleware) TelemetryFrame(ctx context.Context, t time.Time) (b []byte, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Time("query_time", t),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("spacecraft.TelemetryFrame", args...)
			return
		}
		args = append(args, slog.Int("frame_bytes", len(b)))
		lm.logger.Debug("spacecraft.TelemetryFrame", args...)
	}(time.Now())
	return lm.svc.TelemetryFrame(ctx, t)
}

func (lm *loggingMiddleware) TMFrame(ctx context.Context, t time.Time, frameSize int) (b []byte, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Int("frame_size", frameSize),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("spacecraft.TMFrame", args...)
			return
		}
		lm.logger.Debug("spacecraft.TMFrame", args...)
	}(time.Now())
	return lm.svc.TMFrame(ctx, t, frameSize)
}

func (lm *loggingMiddleware) State(ctx context.Context, t time.Time) (st spacecraft.State, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Time("query_time", t),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("spacecraft.State", args...)
			return
		}
		lm.logger.Debug("spacecraft.State", args...)
	}(time.Now())
	return lm.svc.State(ctx, t)
}

func (lm *loggingMiddleware) ExecuteCommand(ctx context.Context, tcFrame []byte) (res spacecraft.CommandResult, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Int("frame_bytes", len(tcFrame)),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("spacecraft.ExecuteCommand", args...)
			return
		}
		args = append(args,
			slog.Uint64("command_id", uint64(res.CommandID)),
			slog.Bool("accepted", res.Accepted),
			slog.String("message", res.Message),
		)
		lm.logger.Info("spacecraft.ExecuteCommand", args...)
	}(time.Now())
	return lm.svc.ExecuteCommand(ctx, tcFrame)
}

func (lm *loggingMiddleware) Windows(ctx context.Context, gsLat, gsLon float64, start, end time.Time, minElevDeg float64) (ws []orbital.ContactWindow, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Float64("gs_lat", gsLat),
			slog.Float64("gs_lon", gsLon),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("spacecraft.Windows", args...)
			return
		}
		args = append(args, slog.Int("window_count", len(ws)))
		lm.logger.Debug("spacecraft.Windows", args...)
	}(time.Now())
	return lm.svc.Windows(ctx, gsLat, gsLon, start, end, minElevDeg)
}

func (lm *loggingMiddleware) Events() []spacecraft.AutonomousEvent {
	return lm.svc.Events()
}

func (lm *loggingMiddleware) PayloadState() *spacecraft.DeployedPayload {
	return lm.svc.PayloadState()
}

func (lm *loggingMiddleware) LastResult() inference.Result {
	return lm.svc.LastResult()
}
