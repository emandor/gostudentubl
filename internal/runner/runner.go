package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/emandor/gostudentubl/internal/moodle"
	"github.com/emandor/gostudentubl/internal/notify"
)

type LLMClient interface {
	GetSuggestion(ctx context.Context, req SuggestionRequest) (SuggestionResponse, error)
}

type SuggestionRequest struct {
	EventType  string
	CourseName string
	ItemName   string
	ItemTitle  string
	Content    string
}

type SuggestionResponse struct {
	Suggestion string
	Model      string
	TokensUsed int
}

type Runner struct {
	Log              zerolog.Logger
	M                *moodle.Client
	Dry              bool
	Conc             int
	Limiter          *rate.Limiter
	Username         string
	Password         string
	WAMe             string
	WAGroup          string
	Timezone         string
	PeriodeMode      string
	AllowedPeriodes  string
	CurrentPeriode   string
	MaxCoursesPerRun int

	NotificationStore         *notify.NotificationStore
	NotificationBatchLimit    int
	NotificationRetentionDays int

	DetailFetchEnabled bool
	DetailFetchLimit   int
	SuggestionEnabled  bool
	LLMClient          LLMClient
}

func (r *Runner) RunAttendance(ctx context.Context) error {
	if err := r.M.Login(ctx, r.Username, r.Password); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	courses, err := r.M.GetCourses(ctx)

	if err != nil {
		return fmt.Errorf("courses: %w", err)
	}

	sort.Slice(courses, func(i, j int) bool { return courses[i].CourseName < courses[j].CourseName })

	now, err := nowInTimezone(r.Timezone, time.Now())
	if err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	allowedPeriodes, err := resolveAllowedPeriodes(r.PeriodeMode, r.AllowedPeriodes, r.CurrentPeriode, now)
	if err != nil {
		return err
	}
	allowedSet := make(map[string]struct{}, len(allowedPeriodes))
	for _, p := range allowedPeriodes {
		allowedSet[p] = struct{}{}
	}

	periodCounts := map[string]int{}
	filteredCourses := make([]moodle.Course, 0, len(courses))
	for _, c := range courses {
		p := normalizePeriode(c.Periode)
		if p == "" {
			periodCounts["<empty>"]++
			continue
		}
		periodCounts[p]++
		if isAllowedPeriode(p, allowedSet) {
			filteredCourses = append(filteredCourses, c)
		}
	}

	r.Log.Info().
		Str("periode_mode", normalizePeriodeMode(r.PeriodeMode)).
		Strs("allowed_periodes", allowedPeriodes).
		Interface("discovered_periodes", periodCounts).
		Int("matched_courses", len(filteredCourses)).
		Int("total_courses", len(courses)).
		Msg("periode filtering result")

	if exceedsMaxCourses(r.MaxCoursesPerRun, len(filteredCourses)) {
		return fmt.Errorf("matched courses exceeded MAX_COURSES_PER_RUN (%d > %d)", len(filteredCourses), r.MaxCoursesPerRun)
	}

	if len(filteredCourses) == 0 {
		r.Log.Warn().
			Strs("allowed_periodes", allowedPeriodes).
			Interface("discovered_periodes", periodCounts).
			Msg("no courses matched periode filter")
		return nil
	}

	if err := r.fetchAssignmentsAndQuizzes(ctx, filteredCourses); err != nil {
		r.Log.Warn().Err(err).Msg("assignment/quiz notification pipeline")
	}

	// Phase 2: detail page fetching
	if r.DetailFetchEnabled && r.NotificationStore != nil {
		if err := r.fetchItemDetails(ctx); err != nil {
			r.Log.Warn().Err(err).Msg("detail page fetch pipeline")
		}
	}

	// Phase 3: deadline reminders
	if r.NotificationStore != nil {
		if err := r.checkDeadlineReminders(ctx, now); err != nil {
			r.Log.Warn().Err(err).Msg("deadline reminder pipeline")
		}
	}

	// Phase 4: LLM suggestions
	if r.SuggestionEnabled && r.LLMClient != nil && r.NotificationStore != nil {
		if err := r.processSuggestions(ctx); err != nil {
			r.Log.Warn().Err(err).Msg("suggestion pipeline")
		}
	}

	var all []moodle.Attendance
	for _, c := range filteredCourses {
		r.Log.Info().Str("course", c.CourseName).Msg("fetching attendance list")
		ats, err := r.M.GetAttendance(ctx, c)
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
	g.SetLimit(effectiveConcurrency(r.Conc))
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
				messageToMe := fmt.Sprintf("✅ Presensi sukses!\n\nMata Kuliah: %s\nPresensi: %s\nJam: %s\nLink: %s", courseName, a.AttendanceName, t, a.AttendanceLink)
				messageToGroup := fmt.Sprintf("🤖 Absen Sodara ☕️\n\nMata Kuliah: %s\nPresensi: %s\nJam: %s\nLink: %s", courseName, a.AttendanceName, t, a.AttendanceLink)

				notify.SendWhatsAppConcurrent([]notify.GroupMessage{
					{Message: messageToMe, GroupID: r.WAMe},
					{Message: messageToGroup, GroupID: r.WAGroup},
				})
				return nil
			}
			return nil
		})
	}
	return g.Wait()
}

func (r *Runner) fetchAssignmentsAndQuizzes(ctx context.Context, courses []moodle.Course) error {
	totalAssignments := 0
	totalQuizzes := 0
	events := make([]notify.NotificationEvent, 0, len(courses)*2)
	for _, c := range courses {
		r.Log.Info().Str("course", c.CourseName).Msg("fetching assignment list")
		assignments, err := r.M.GetAssignments(ctx, c)
		if err != nil {
			r.Log.Warn().Err(err).Str("course", c.CourseName).Msg("assignment list")
			continue
		}
		totalAssignments += len(assignments)
		for _, a := range assignments {
			dueDateRaw := a.DueDate
			dueDateParsed := ""
			if dueDateRaw != "" {
				if t, err := moodle.ParseMoodleDate(dueDateRaw); err == nil {
					dueDateParsed = t.UTC().Format(time.RFC3339)
				}
			}
			events = append(events, notify.NotificationEvent{
				EventType:        notify.NotificationTypeAssignment,
				CourseID:         c.CourseID,
				CourseName:       c.CourseName,
				ItemTitle:        a.Title,
				ItemName:         a.AssignmentName,
				ItemLink:         a.AssignmentLink,
				ItemID:           a.AssignmentID,
				DueDate:          dueDateRaw,
				SubmissionStatus: a.SubmissionStatus,
				Grade:            a.Grade,
				DueDateParsed:    dueDateParsed,
			})
			r.Log.Info().
				Str("course", a.Course.CourseName).
				Str("title", a.Title).
				Str("assignment", a.AssignmentName).
				Str("link", a.AssignmentLink).
				Msg("assignment found")
		}

		r.Log.Info().Str("course", c.CourseName).Msg("fetching quiz list")
		quizzes, err := r.M.GetQuizzes(ctx, c)
		if err != nil {
			r.Log.Warn().Err(err).Str("course", c.CourseName).Msg("quiz list")
			continue
		}
		totalQuizzes += len(quizzes)
		for _, q := range quizzes {
			closeDateRaw := q.CloseDate
			dueDateParsed := ""
			if closeDateRaw != "" {
				if t, err := moodle.ParseMoodleDate(closeDateRaw); err == nil {
					dueDateParsed = t.UTC().Format(time.RFC3339)
				}
			}
			events = append(events, notify.NotificationEvent{
				EventType:        notify.NotificationTypeQuiz,
				CourseID:         c.CourseID,
				CourseName:       c.CourseName,
				ItemTitle:        q.Title,
				ItemName:         q.QuizName,
				ItemLink:         q.QuizLink,
				ItemID:           q.QuizID,
				DueDate:          closeDateRaw,
				SubmissionStatus: "",
				Grade:            q.Grade,
				DueDateParsed:    dueDateParsed,
			})
			r.Log.Info().
				Str("course", q.Course.CourseName).
				Str("title", q.Title).
				Str("quiz", q.QuizName).
				Str("link", q.QuizLink).
				Msg("quiz found")
		}
	}
	r.Log.Info().
		Int("total_assignments", totalAssignments).
		Int("total_quizzes", totalQuizzes).
		Msg("assignment and quiz scan completed")

	if r.NotificationStore == nil {
		r.Log.Warn().Msg("notification store not configured; skipping assignment/quiz whatsapp dispatch")
		return nil
	}
	if err := r.NotificationStore.UpsertEvents(ctx, events); err != nil {
		return fmt.Errorf("upsert notification events: %w", err)
	}

	if r.Dry {
		r.Log.Info().Int("queued_events", len(events)).Msg("dry-run: queued assignment/quiz events without dispatch")
		return nil
	}

	pending, err := r.NotificationStore.ListPending(ctx, r.NotificationBatchLimit)
	if err != nil {
		return fmt.Errorf("list pending notification events: %w", err)
	}
	r.Log.Info().Int("pending_notifications", len(pending)).Msg("dispatching assignment/quiz notifications")

	for _, item := range pending {
		targets := r.notificationTargets(item)
		if len(targets) == 0 {
			if err := r.NotificationStore.MarkFailed(ctx, item.ID, fmt.Errorf("no whatsapp targets configured")); err != nil {
				r.Log.Warn().Err(err).Int64("notification_id", item.ID).Msg("failed to mark notification as failed")
			}
			continue
		}

		if err := notify.SendWhatsAppReliable(targets); err != nil {
			r.Log.Warn().Err(err).Int64("notification_id", item.ID).Str("type", item.EventType).Str("name", item.ItemName).Msg("notification send failed")
			if markErr := r.NotificationStore.MarkFailed(ctx, item.ID, err); markErr != nil {
				r.Log.Warn().Err(markErr).Int64("notification_id", item.ID).Msg("failed to update failed notification")
			}
			continue
		}

		if err := r.NotificationStore.MarkNotified(ctx, item.ID, time.Now()); err != nil {
			r.Log.Warn().Err(err).Int64("notification_id", item.ID).Msg("failed to mark notification as notified")
			continue
		}
		r.Log.Info().Int64("notification_id", item.ID).Str("type", item.EventType).Str("name", item.ItemName).Msg("notification sent")
	}

	if r.NotificationRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -r.NotificationRetentionDays)
		deleted, err := r.NotificationStore.PruneNotifiedBefore(ctx, cutoff)
		if err != nil {
			r.Log.Warn().Err(err).Msg("failed to prune old notified records")
		} else if deleted > 0 {
			r.Log.Info().Int64("deleted_records", deleted).Msg("pruned old notified records")
		}
	}

	return nil
}

func (r *Runner) notificationTargets(item notify.PendingNotification) []notify.GroupMessage {
	message := buildNotificationMessage(item)
	targets := make([]notify.GroupMessage, 0, 2)
	if strings.TrimSpace(r.WAMe) != "" {
		targets = append(targets, notify.GroupMessage{Message: message, GroupID: r.WAMe})
	}
	if strings.TrimSpace(r.WAGroup) != "" {
		targets = append(targets, notify.GroupMessage{Message: "🤖 " + message, GroupID: r.WAGroup})
	}
	return targets
}

func buildNotificationMessage(item notify.PendingNotification) string {
	label := notificationLabel(item.EventType)
	title := strings.TrimSpace(item.ItemTitle)
	if title == "" {
		title = "-"
	}
	msg := fmt.Sprintf(
		"📚 %s baru terdeteksi!\n\nMata Kuliah: %s\nTopik: %s\nItem: %s",
		label,
		item.CourseName,
		title,
		item.ItemName,
	)
	if item.DueDate != "" {
		msg += fmt.Sprintf("\nDue: %s", item.DueDate)
	}
	if item.SubmissionStatus != "" {
		msg += fmt.Sprintf("\nStatus: %s", item.SubmissionStatus)
	}
	if item.Grade != "" && item.Grade != "-" {
		msg += fmt.Sprintf("\nGrade: %s", item.Grade)
	}
	msg += fmt.Sprintf("\nLink: %s", item.ItemLink)
	return msg
}

func notificationLabel(eventType string) string {
	switch eventType {
	case notify.NotificationTypeAssignment:
		return "Assignment"
	case notify.NotificationTypeQuiz:
		return "Quiz"
	default:
		return "Update"
	}
}

func effectiveConcurrency(conc int) int {
	if conc < 1 {
		return 1
	}
	return conc
}

func exceedsMaxCourses(maxCoursesPerRun, matchedCourses int) bool {
	if maxCoursesPerRun <= 0 {
		return false
	}
	return matchedCourses > maxCoursesPerRun
}
