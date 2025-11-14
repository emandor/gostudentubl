package schedule

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

type JobRunner interface {
	RunAttendance(ctx context.Context) error
}

type Jobs struct {
	Cron *cron.Cron
	Log  zerolog.Logger
}

func New(tz string, logger zerolog.Logger) *Jobs {
	loc, _ := time.LoadLocation(tz)
	return &Jobs{Cron: cron.New(cron.WithLocation(loc)), Log: logger}
}

func (j *Jobs) Add(spec string, r JobRunner) error {
	_, err := j.Cron.AddFunc(spec, func() {
		timeNow := time.Now().In(j.Cron.Location())
		j.Log.Info().Str("time", timeNow.Format(time.RFC3339)).Msg("starting scheduled attendance run")
		ctx := context.Background()
		if err := r.RunAttendance(ctx); err != nil {
			j.Log.Error().Err(err).Str("time", timeNow.Format(time.RFC3339)).Msg("scheduled attendance run failed")
		} else {
			j.Log.Info().Str("time", timeNow.Format(time.RFC3339)).Msg("scheduled attendance run completed successfully")
		}
	})
	return err
}

func (j *Jobs) Start() { j.Cron.Start() }
func (j *Jobs) Stop()  { ctx := j.Cron.Stop(); <-ctx.Done() }
