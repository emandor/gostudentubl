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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		// TODO: fix small jitter to avoid looking botty
		// time.Sleep(time.Duration(rand.Intn(4000)) * time.Millisecond)
		log := j.Log.With().Str("job", "attendance").Logger()
		timeNow := time.Now().In(j.Cron.Location())
		log = log.With().Str("time", timeNow.Format(time.RFC3339)).Logger()
		log.Info().Msg("🚀 starting attendance job")
		if err := r.RunAttendance(ctx); err != nil {
			failedTime := time.Now().In(j.Cron.Location())
			log = log.With().Str("failed_time", failedTime.Format(time.RFC3339)).Logger()

			log.Error().Err(err).Msg("attendance job failed")
			return
		}
	})
	return err
}

func (j *Jobs) Start() { j.Cron.Start() }
func (j *Jobs) Stop()  { ctx := j.Cron.Stop(); <-ctx.Done() }
