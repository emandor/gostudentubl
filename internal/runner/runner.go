package runner

import (
	"context"
	"fmt"
	"os"
	"sort"
	// "time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/emandor/gostudentubl/internal/moodle"
)

type Runner struct {
	Log zerolog.Logger
	M   *moodle.Client
	// TODO
	// WA      notify.Notifier
	Dry     bool
	Conc    int
	Limiter *rate.Limiter
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

	var all []moodle.Attendance
	for _, c := range courses {
		// log some info about the course
		r.Log.Info().Str("course", c.CourseName).Msg("fetching attendance list")
		ats, err := r.M.GetAttendance(ctx, fmt.Sprintf("%d", c.CourseID))
		if err != nil {
			r.Log.Warn().Err(err).Str("course", c.CourseName).Msg("attendance list")
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
	g.SetLimit(max(1, r.Conc))
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
				// t := time.Now().Format(time.RFC3339)
				r.Log.Info().Str("courseId", a.CourseID).Str("attendance", a.AttendanceName).Msg("submitted")
				// _ = r.WA.Send(ctx, "Attendance submitted", fmt.Sprintf("✅ %s at %s", a.AttendanceName, t))
			}
			return nil
		})
	}
	return g.Wait()
}
