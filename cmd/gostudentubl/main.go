package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/time/rate"

	"github.com/emandor/gostudentubl/internal/config"
	"github.com/emandor/gostudentubl/internal/httpx"
	"github.com/emandor/gostudentubl/internal/llm"
	"github.com/emandor/gostudentubl/internal/moodle"
	"github.com/emandor/gostudentubl/internal/notify"
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
	m.Base.AssignmentListURL = cfg.AssignmentListURL
	m.Base.QuizListURL = cfg.QuizListURL
	if m.Base.AssignmentListURL == "" {
		m.Base.AssignmentListURL = strings.Replace(cfg.AttendanceListURL, "/mod/attendance/", "/mod/assign/", 1)
	}
	if m.Base.QuizListURL == "" {
		m.Base.QuizListURL = strings.Replace(cfg.AttendanceListURL, "/mod/attendance/", "/mod/quiz/", 1)
	}
	m.Base.AssignmentDetailURL = cfg.AssignmentDetailURL
	m.Base.QuizDetailURL = cfg.QuizDetailURL
	if m.Base.AssignmentDetailURL == "" {
		m.Base.AssignmentDetailURL = strings.Replace(m.Base.AssignmentListURL, "/mod/assign/index.php", "/mod/assign/view.php", 1)
	}
	if m.Base.QuizDetailURL == "" {
		m.Base.QuizDetailURL = strings.Replace(m.Base.QuizListURL, "/mod/quiz/index.php", "/mod/quiz/view.php", 1)
	}
	m.Base.AttendanceURL = cfg.AttendanceURL
	m.Base.AttendanceFormURL = cfg.AttendanceFormURL
	// Derive detail page URLs from list URLs
	m.Base.AssignmentDetailURL = strings.Replace(m.Base.AssignmentListURL, "/index.php", "/view.php", 1)
	m.Base.QuizDetailURL = strings.Replace(m.Base.QuizListURL, "/index.php", "/view.php", 1)

	llmClient := &llm.Client{
		Endpoint: cfg.OpenRouterEndpoint,
		APIKey:   cfg.OpenRouterAPIKey,
		Model:    cfg.OpenRouterModel,
		HC:       hc,
	}

	store, err := notify.NewNotificationStore(cfg.NotificationDBPath)
	if err != nil {
		log.Fatal().Err(err).Msg("notification store")
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("closing notification store")
		}
	}()

	r := &runner.Runner{
		Log:              log,
		M:                m,
		Dry:              cfg.DryRun,
		Conc:             cfg.Concurrency,
		Limiter:          rate.NewLimiter(rate.Limit(cfg.RatePerSec), cfg.RateBurst),
		Username:         cfg.Username,
		Password:         cfg.Password,
		WAMe:             cfg.WAMe,
		WAGroup:          cfg.WaGroup,
		Timezone:         cfg.Timezone,
		PeriodeMode:      cfg.PeriodeMode,
		AllowedPeriodes:  cfg.AllowedPeriodes,
		CurrentPeriode:   cfg.CurrentPeriode,
		MaxCoursesPerRun: cfg.MaxCoursesPerRun,

		NotificationStore:         store,
		NotificationBatchLimit:    cfg.NotificationBatchLimit,
		NotificationRetentionDays: cfg.NotificationRetentionDays,
<<<<<<< Updated upstream

		DetailFetchEnabled: cfg.DetailFetchEnabled,
		DetailFetchLimit:   cfg.DetailFetchLimit,
		SuggestionEnabled:  cfg.SuggestionEnabled,
	}

	// Initialize LLM client if suggestions enabled
	if cfg.SuggestionEnabled && cfg.OpenRouterAPIKey != "" {
		llmClient := &llm.Client{
			Endpoint: cfg.OpenRouterEndpoint,
			APIKey:   cfg.OpenRouterAPIKey,
			Model:    cfg.OpenRouterModel,
			HC:       &http.Client{Timeout: cfg.RequestTimeout()},
		}
		r.LLMClient = llmClient
		log.Info().Str("model", cfg.OpenRouterModel).Msg("LLM suggestion system enabled")
=======
		DetailFetchEnabled:        cfg.DetailFetchEnabled,
		DetailFetchLimit:          cfg.DetailFetchLimit,
		SuggestionEnabled:         cfg.SuggestionEnabled,
		SuggestionLimit:           cfg.SuggestionLimit,
		LLM:                       llmClient,
>>>>>>> Stashed changes
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
			if err := r.RunAttendance(context.Background()); err != nil {
				log.Error().Err(err).Msg("manual attendance run failed")
			}
		}
	}()

	// graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	jobs.Stop()
	log.Info().Msg("shutdown")
}
