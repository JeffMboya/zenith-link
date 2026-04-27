// Command spacecraft runs the Zenith-Link spacecraft telemetry service.
//
// Configuration is read from environment variables:
//
//	SPACECRAFT_ADDR       HTTP listen address (default: :8080)
//	SPACECRAFT_SCID       10-bit Spacecraft ID in decimal (default: 90)
//	SPACECRAFT_VCID       6-bit Virtual Channel ID (default: 0)
//	SPACECRAFT_APID       11-bit Application Process ID (default: 256)
//	ZENITH_HMAC_KEY       HMAC-SHA256 key (required)
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"math"

	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/absmach/zenith-link/spacecraft"
	"github.com/absmach/zenith-link/spacecraft/api"
	"github.com/absmach/zenith-link/spacecraft/middleware"
	"github.com/caarlos0/env/v11"
)

type config struct {
	Addr    string `env:"SPACECRAFT_ADDR" envDefault:":8080"`
	SCID    uint16 `env:"SPACECRAFT_SCID" envDefault:"90"`
	VCID    uint8  `env:"SPACECRAFT_VCID" envDefault:"0"`
	APID    uint16 `env:"SPACECRAFT_APID" envDefault:"256"`
	HMACKey string `env:"ZENITH_HMAC_KEY,required"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		logger.Error("failed to parse config", slog.Any("error", err))
		os.Exit(1)
	}

	// ISS-like orbit for demonstration; in production this would come from a
	// TLE feed or mission database.
	elem := orbital.Elements{
		SemiMajorAxis: 6_788_000,
		Eccentricity:  0.0001,
		Inclination:   51.6 * math.Pi / 180,
		RAAN:          0.0,
		ArgPerigee:    0.0,
		MeanAnomaly:   0.0,
		Epoch:         time.Now().UTC(),
	}

	svcCfg := spacecraft.Config{
		SCID:          cfg.SCID,
		VCID:          cfg.VCID,
		TelemetryAPID: cfg.APID,
		HMACKey:       []byte(cfg.HMACKey),
		Elements:      elem,
	}

	svc := middleware.NewLogging(spacecraft.New(svcCfg), logger)
	router := api.NewRouter(svc)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("spacecraft service starting", slog.String("addr", cfg.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
}
