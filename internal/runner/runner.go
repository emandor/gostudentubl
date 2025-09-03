package runner

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/emandor/gostudentubl/internal/moodle"
	"github.com/emandor/gostudentubl/internal/notify"
)

type Runner struct {
	Log            zerolog.Logger
	M              *moodle.Client
	Dry            bool
	Conc           int
	CurrentPeriode string
	Limiter        *rate.Limiter
}

func (r *Runner) RunAttendance(ctx context.Context) error {
	if err := r.M.Login(ctx /* env */, os.Getenv("USERNAME"), os.Getenv("PASSWORD")); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	courses, err := r.M.GetCourses(ctx)

	if err != nil {
		return fmt.Errorf("courses: %w", err)
	}

	// Group/filter by current periode if desired (simple example keeps all)
	sort.Slice(courses, func(i, j int) bool { return courses[i].CourseName < courses[j].CourseName })

	currentPeriode := r.CurrentPeriode
	var all []moodle.Attendance
	for _, c := range courses {
		// log some info about the course
		r.Log.Info().Str("course", c.CourseName).Msg("fetching attendance list")
		ats, err := r.M.GetAttendance(ctx, c)
		if err != nil {
			r.Log.Warn().Err(err).Str("course", c.CourseName).Msg("attendance list")
			continue
		}

		if c.Periode != currentPeriode {
			r.Log.Info().Str("course", c.CourseName).Str("periode", c.Periode).Msg("skipping not current periode")
			r.Log.Info().Msgf("current periode is %q", currentPeriode)
			continue
		}
		for _, a := range ats {
			all = append(all, a)
		}
	}
	if len(all) == 0 {
		r.Log.Info().Msg("no attendance found")
		return nil
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(max(5, r.Conc))
	for i := range all {
		a := all[i]
		g.Go(func() error {
			if err := r.Limiter.Wait(ctx); err != nil {
				return err
			}
			vi, err := r.M.ViewAttendanceByID(ctx, a.AttendanceID)
			if err != nil {
				r.Log.Warn().Err(err).Str("att", a.AttendanceName).Msg("view")
				return nil
			}
			if r.Dry {
				r.Log.Info().Str("att", a.AttendanceName).Msg("dry-run skip submit")
				return nil
			}
			fi, err := r.M.GetFormInfo(ctx, vi.SubmitLink, vi.SessionID, vi.SessKey)
			if err != nil {
				r.Log.Warn().Err(err).Str("att", a.AttendanceName).Msg("form")
				return nil
			}
			if err := r.M.SubmitAttendance(ctx, r.M.Base.AttendanceFormURL, fi); err != nil {
				r.Log.Warn().Err(err).Str("att", a.AttendanceName).Msg("submit")
				return nil
			}
			done, err := r.M.CheckSubmitted(ctx, a.AttendanceID)
			if err != nil {
				r.Log.Warn().Err(err).Str("att", a.AttendanceName).Msg("check")
				return nil
			}
			if done {
				t := time.Now().Format(time.RFC3339)
				courseName := a.Course.CourseName
				r.Log.Info().Str("at", t).Str("course", courseName).Str("att", a.AttendanceName).Msg("✅ attendance submitted")
				messageToMe := fmt.Sprintf("✅ Presensi sukses!\n\nMata Kuliah: %s\nPresensi: %s\nJam: %s", courseName, a.AttendanceName, t)
				messageToGroup := fmt.Sprintf("🤖 Absen Sodara ☕️\n\nMata Kuliah: %s\nPresensi: %s\nJam: %s", courseName, a.AttendanceName, t)

				notify.SendWhatsAppConcurrent([]notify.GroupMessage{
					{Message: messageToMe, GroupID: os.Getenv("WA_ME")},
					{Message: messageToGroup, GroupID: os.Getenv("WA_GROUP")},
				})
				return nil
			}
			return nil
		})
	}
	return g.Wait()
}
