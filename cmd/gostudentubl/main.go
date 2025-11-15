package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/time/rate"

	"github.com/emandor/gostudentubl/internal/config"
	"github.com/emandor/gostudentubl/internal/httpx"
	"github.com/emandor/gostudentubl/internal/moodle"
	"github.com/emandor/gostudentubl/internal/runner"
	"github.com/emandor/gostudentubl/internal/schedule"
	"github.com/emandor/gostudentubl/internal/telemetry"
)

func main() {
	log := telemetry.NewLogger()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("config")
	}

	hc, err := httpx.NewHTTP(cfg.RequestTimeout())
	if err != nil {
		log.Fatal().Err(err).Msg("http")
	}

	m := &moodle.Client{HC: hc, UA: "Mozilla/5.0"}
	m.Base.LoginURL = cfg.LoginURL
	m.Base.CoursesURL = cfg.CoursesURL
	m.Base.AttendanceListURL = cfg.AttendanceListURL
	m.Base.AttendanceURL = cfg.AttendanceURL
	m.Base.AttendanceFormURL = cfg.AttendanceFormURL

	r := &runner.Runner{
		Log:            log,
		CurrentPeriode: cfg.CurrentPeriode,
		M:              m,
		Dry:            cfg.DryRun,
		Conc:           cfg.Concurrency,
		Limiter:        rate.NewLimiter(rate.Limit(cfg.RatePerSec), cfg.RateBurst),
	}

	jobs := schedule.New(cfg.Timezone, log)

	errWeekDay := jobs.Add(cfg.CronWeekday, r)
	if errWeekDay != nil {
		log.Fatal().Err(errWeekDay).Msg("adding weekday job")
	}

	errWeekEnd := jobs.Add(cfg.CronWeekend, r)
	if errWeekEnd != nil {
		log.Fatal().Err(errWeekEnd).Msg("adding weekend job")
	}

	jobs.Start()
	log.Info().Str("tz", cfg.Timezone).Msg("🤖 live! beep beep...")

	// also allow single run via SIGUSR1
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGUSR1)
		for range ch {
			_ = r.RunAttendance(context.Background())
		}
	}()

	// graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	jobs.Stop()
	log.Info().Msg("shutdown")
}
