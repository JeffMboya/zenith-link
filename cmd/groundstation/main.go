// Command groundstation runs the Zenith-Link ground station service.
//
// Configuration is read from environment variables:
//
//	GROUNDSTATION_ADDR    HTTP listen address (default: :8081)
//	ZENITH_HMAC_KEY       HMAC-SHA256 shared key (required, must match spacecraft)
//	GS_MAX_SUBSCRIBERS    Maximum concurrent WebSocket subscribers (default: 64)
//	GS_LAT                Ground station geodetic latitude  [degrees] (default: -1.2864, Nairobi)
//	GS_LON                Ground station geodetic longitude [degrees] (default: 36.8172, Nairobi)
//	SC_ADDR               Spacecraft service base URL for command forwarding (default: http://spacecraft:8080)
//	SC_SCID               Spacecraft CCSDS SCID used in forwarded TC frames (default: 90)
//	SC_VCID               Spacecraft virtual channel ID used in TC frames (default: 0)
//	STATIC_DIR            Path to compiled frontend dist (enables prod mode with proxying)
//	RELAY1_ADDR           Relay-1 base URL (used in prod mode, default: http://relay:8082)
//	RELAY2_ADDR           Relay-2 base URL (used in prod mode, default: http://relay2:8083)
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/absmach/zenith-link/groundstation"
	gsapi "github.com/absmach/zenith-link/groundstation/api"
	gsmiddleware "github.com/absmach/zenith-link/groundstation/middleware"
	"github.com/caarlos0/env/v11"
)

type config struct {
	Addr           string  `env:"GROUNDSTATION_ADDR" envDefault:":8081"`
	HMACKey        string  `env:"ZENITH_HMAC_KEY,required"`
	MaxSubscribers int     `env:"GS_MAX_SUBSCRIBERS" envDefault:"64"`
	GSLat          float64 `env:"GS_LAT"  envDefault:"-1.2864"`
	GSLon          float64 `env:"GS_LON"  envDefault:"36.8172"`
	SCAddr         string  `env:"SC_ADDR" envDefault:"http://spacecraft:8080"`
	SCSCID         uint16  `env:"SC_SCID" envDefault:"90"`
	SCVCID         uint8   `env:"SC_VCID" envDefault:"0"`
	StaticDir      string  `env:"STATIC_DIR"   envDefault:""`
	Relay1Addr     string  `env:"RELAY1_ADDR"  envDefault:"http://relay:8082"`
	Relay2Addr     string  `env:"RELAY2_ADDR"  envDefault:"http://relay2:8083"`
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

	svcCfg := groundstation.Config{
		HMACKey:        []byte(cfg.HMACKey),
		MaxSubscribers: cfg.MaxSubscribers,
	}

	routerCfg := gsapi.RouterConfig{
		SpacecraftAddr: cfg.SCAddr,
		SCID:           cfg.SCSCID,
		VCID:           cfg.SCVCID,
		GSLat:          cfg.GSLat,
		GSLon:          cfg.GSLon,
		StaticDir:      cfg.StaticDir,
		Relay1Addr:     cfg.Relay1Addr,
		Relay2Addr:     cfg.Relay2Addr,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc := gsmiddleware.NewLogging(groundstation.New(svcCfg), logger)
	router := gsapi.NewRouter(svc, routerCfg)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("groundstation service starting",
			slog.String("addr", cfg.Addr),
			slog.Float64("gs_lat", cfg.GSLat),
			slog.Float64("gs_lon", cfg.GSLon),
			slog.String("sc_addr", cfg.SCAddr),
		)
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
