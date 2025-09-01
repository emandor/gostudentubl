package config

import (
	"time"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	Timezone string `env:"TIMEZONE,required"`
	Username string `env:"USERNAME,required"`
	Password string `env:"PASSWORD,required"`

	LoginURL          string `env:"LOGIN_URL,required"`
	CoursesURL        string `env:"COURSES_URL,required"`
	AttendanceListURL string `env:"ATTENDANCE_LIST_URL,required"`
	AttendanceURL     string `env:"ATTENDANCE_URL,required"`
	AttendanceFormURL string `env:"ATTENDANCE_FORM_URL,required"`
	CurrentPeriode    string `env:"CURRENT_PERIODE"`

	WAEndpoint string `env:"WA_ENDPOINT,required"`
	WAToken    string `env:"WA_TOKEN,required"`
	WATarget   string `env:"WA_TARGET,required"`

	CronWeekday string `env:"CRON_WEEKDAY"`
	CronWeekend string `env:"CRON_WEEKEND"`

	Concurrency       int     `env:"CONCURRENCY"`
	RatePerSec        float64 `env:"RATE_PER_SEC"`
	RateBurst         int     `env:"RATE_BURST"`
	RequestTimeoutSec int     `env:"REQUEST_TIMEOUT_SEC"`
	DryRun            bool    `env:"DRY_RUN"`
}

func Load() (Config, error) {
	cfg := Config{
		Timezone:          "Asia/Jakarta",
		CronWeekday:       "1 8,12,13,14,19 * * 1-5",
		CronWeekend:       "0 8,9,11,14,16 * * 6",
		Concurrency:       4,
		RatePerSec:        1,
		RateBurst:         2,
		RequestTimeoutSec: 15,
	}
	return cfg, env.Parse(&cfg)
}

func (c Config) RequestTimeout() time.Duration {
	if c.RequestTimeoutSec <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.RequestTimeoutSec) * time.Second
}
