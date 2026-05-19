package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	Timezone string `env:"TIMEZONE,required"`
	Username string `env:"USERNAME,required"`
	Password string `env:"PASSWORD,required"`

	LoginURL            string `env:"LOGIN_URL,required"`
	CoursesURL          string `env:"COURSES_URL,required"`
	AttendanceListURL   string `env:"ATTENDANCE_LIST_URL,required"`
	AssignmentListURL   string `env:"ASSIGNMENT_LIST_URL"`
	QuizListURL         string `env:"QUIZ_LIST_URL"`
	AssignmentDetailURL string `env:"ASSIGNMENT_DETAIL_URL"`
	QuizDetailURL       string `env:"QUIZ_DETAIL_URL"`
	AttendanceURL       string `env:"ATTENDANCE_URL,required"`
	AttendanceFormURL   string `env:"ATTENDANCE_FORM_URL,required"`
	CurrentPeriode      string `env:"CURRENT_PERIODE"`

	PeriodeMode     string `env:"PERIODE_MODE"`
	AllowedPeriodes string `env:"ALLOWED_PERIODES"`

	WAEndpoint string `env:"WA_ENDPOINT,required"`
	WAToken    string `env:"WA_TOKEN,required"`
	WAMe       string `env:"WA_ME"`
	WaGroup    string `env:"WA_GROUP"`

	NotificationDBPath        string `env:"NOTIFICATION_DB_PATH"`
	NotificationBatchLimit    int    `env:"NOTIFICATION_BATCH_LIMIT"`
	NotificationRetentionDays int    `env:"NOTIFICATION_RETENTION_DAYS"`
	DetailFetchEnabled        bool   `env:"DETAIL_FETCH_ENABLED"`
	DetailFetchLimit          int    `env:"DETAIL_FETCH_LIMIT"`

	OpenRouterEndpoint string `env:"OPENROUTER_ENDPOINT"`
	OpenRouterAPIKey   string `env:"OPENROUTER_API_KEY"`
	OpenRouterModel    string `env:"OPENROUTER_MODEL"`
	SuggestionEnabled  bool   `env:"SUGGESTION_ENABLED"`
	SuggestionLimit    int    `env:"SUGGESTION_LIMIT"`

	DraftEnabled     bool   `env:"DRAFT_ENABLED"`
	DraftSolver      string `env:"DRAFT_SOLVER"`    // legacy, used when DraftProviders is empty
	DraftProviders   string `env:"DRAFT_PROVIDERS"` // comma-separated: "copilot,openrouter,codex"
	DraftRetryMax    int    `env:"DRAFT_RETRY_MAX"` // max attempts per provider (default 2)
	DraftLimit       int    `env:"DRAFT_LIMIT"`
	DraftTmuxSession string `env:"DRAFT_TMUX_SESSION"`

	RunLockEnabled      bool   `env:"RUN_LOCK_ENABLED"`
	RunLockTTLMinutes   int    `env:"RUN_LOCK_TTL_MINUTES"`
	DailySummaryEnabled bool   `env:"DAILY_SUMMARY_ENABLED"`
	CronDailySummary    string `env:"CRON_DAILY_SUMMARY"`

	CronWeekday string `env:"CRON_WEEKDAY"`
	CronWeekend string `env:"CRON_WEEKEND"`

	Concurrency       int     `env:"CONCURRENCY"`
	RatePerSec        float64 `env:"RATE_PER_SEC"`
	RateBurst         int     `env:"RATE_BURST"`
	MaxCoursesPerRun  int     `env:"MAX_COURSES_PER_RUN"`
	RequestTimeoutSec int     `env:"REQUEST_TIMEOUT_SEC"`
	DryRun            bool    `env:"DRY_RUN"`
}

func Load() (Config, error) {
	cfg := Config{
		Timezone:                  "Asia/Jakarta",
		CronWeekday:               "1 8,12,13,14,19 * * 1-5",
		CronWeekend:               "0 8,9,11,14,16 * * 6",
		PeriodeMode:               "auto",
		Concurrency:               4,
		RatePerSec:                1,
		RateBurst:                 2,
		MaxCoursesPerRun:          0,
		NotificationDBPath:        "notifications.db",
		NotificationBatchLimit:    100,
		NotificationRetentionDays: 30,
		DetailFetchEnabled:        true,
		DetailFetchLimit:          10,
		OpenRouterEndpoint:        "https://openrouter.ai/api/v1/chat/completions",
		OpenRouterModel:           "anthropic/claude-sonnet-4-20250514",
		SuggestionEnabled:         false,
		SuggestionLimit:           5,
		DraftEnabled:              true,
		DraftSolver:               "openrouter-with-copilot-fallback",
		DraftProviders:            "copilot,openrouter,codex",
		DraftRetryMax:             2,
		DraftLimit:                3,
		DraftTmuxSession:          "copilot-solver",
		RunLockEnabled:            true,
		RunLockTTLMinutes:         15,
		DailySummaryEnabled:       true,
		CronDailySummary:          "30 21 * * *",
		RequestTimeoutSec:         15,
	}
	if err := env.Parse(&cfg); err != nil {
		return cfg, err
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return cfg, fmt.Errorf("invalid TIMEZONE %q: %w", cfg.Timezone, err)
	}
	return cfg, nil
}

func (c Config) RequestTimeout() time.Duration {
	if c.RequestTimeoutSec <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.RequestTimeoutSec) * time.Second
}
