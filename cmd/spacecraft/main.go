// Command spacecraft runs the Satlyt Demo spacecraft telemetry service.
//
// Configuration is read from environment variables:
//
//	SPACECRAFT_ADDR       HTTP listen address (default: :8080)
//	SPACECRAFT_SCID       10-bit Spacecraft ID in decimal (default: 90)
//	SPACECRAFT_VCID       6-bit Virtual Channel ID (default: 0)
//	SPACECRAFT_APID       11-bit Application Process ID (default: 256)
//	SATLYT_HMAC_KEY       HMAC-SHA256 key (required)
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

	"github.com/absmach/satlyt-demo/pkg/orbital"
	"github.com/absmach/satlyt-demo/spacecraft"
	"github.com/absmach/satlyt-demo/spacecraft/api"
	"github.com/absmach/satlyt-demo/spacecraft/middleware"
	"github.com/caarlos0/env/v11"
)

type config struct {
	Addr    string `env:"SPACECRAFT_ADDR" envDefault:":8080"`
	SCID    uint16 `env:"SPACECRAFT_SCID" envDefault:"90"`
	VCID    uint8  `env:"SPACECRAFT_VCID" envDefault:"0"`
	APID    uint16 `env:"SPACECRAFT_APID" envDefault:"256"`
	HMACKey string `env:"SATLYT_HMAC_KEY,required"`
}

func adjustForEarlyContact(elem orbital.Elements, gsLat, gsLon, minElevDeg, targetLeadSec float64, logger *slog.Logger) orbital.Elements {
	now := time.Now().UTC()
	windows, err := orbital.ContactWindows(elem, gsLat, gsLon, now, now.Add(24*time.Hour), minElevDeg)
	if err != nil || len(windows) == 0 {
		logger.Warn("spacecraft: startup contact scan failed — using default mean anomaly", slog.Any("error", err))
		return elem
	}
	timeToAOS := windows[0].AOS.Sub(now).Seconds()
	if timeToAOS <= targetLeadSec {
		logger.Info("spacecraft: first Nairobi contact within target lead",
			slog.Float64("time_to_aos_sec", timeToAOS))
		return elem
	}
	const mu = 3.986004418e14
	a := elem.SemiMajorAxis
	T := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	advance := timeToAOS - targetLeadSec
	elem.MeanAnomaly = math.Mod(elem.MeanAnomaly+2*math.Pi*advance/T, 2*math.Pi)
	logger.Info("spacecraft: mean anomaly adjusted for early Nairobi contact",
		slog.Float64("original_aos_sec", timeToAOS),
		slog.Float64("target_lead_sec", targetLeadSec),
		slog.Float64("advance_sec", advance))
	return elem
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

	elem := adjustForEarlyContact(orbital.Elements{
		SemiMajorAxis: 6_878_000,
		Eccentricity:  0.0001,
		Inclination:   97.4 * math.Pi / 180,
		RAAN:          0.0,
		ArgPerigee:    0.0,
		MeanAnomaly:   0.0,
		Epoch:         time.Now().UTC(),
	}, -1.2864, 36.8172, 5.0, 600.0, logger)

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
