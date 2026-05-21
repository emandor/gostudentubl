package runner

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/emandor/gostudentubl/internal/llm"
	"github.com/emandor/gostudentubl/internal/moodle"
	"github.com/emandor/gostudentubl/internal/notify"
	"github.com/emandor/gostudentubl/internal/solver"
)

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

	NotificationStore             *notify.NotificationStore
	NotificationBatchLimit        int
	NotificationRetentionDays     int
	DetailFetchEnabled            bool
	DetailFetchLimit              int
	SuggestionEnabled             bool
	SuggestionLimit               int
	LLMClient                     llm.LLMClient
	DraftEnabled                  bool
	DraftSolver                   string
	DraftLimit                    int
	DraftTmuxSession              string
	Solver                        solver.Solver
	MultiSolver                   *solver.MultiSolver
	RunLockEnabled                bool
	RunLockTTL                    time.Duration
	MaterialSyncEnabled           bool
	MaterialSyncLimit             int
	MaterialCacheDir              string
	MaterialMaxFileMB             int
	PDFGenerationEnabled          bool
	PDFCacheDir                   string
	PDFStudentName                string
	PDFStudentNIM                 string
	ChromiumPath                  string
	SubmissionEnabled             bool
	SubmissionDeadlineWindowHours int

	sendWhatsApp func([]notify.GroupMessage) error
}

type runStats struct {
	TotalCourses        int
	MatchedCourses      int
	AttendanceFound     int
	AttendanceSubmitted int
	AssignmentsFound    int
	QuizzesFound        int
	NotificationsSent   int
	RemindersSent       int
	DraftsReady         int
}

func (r *Runner) RunAttendance(ctx context.Context) (err error) {
	const jobName = "attendance"
	owner := runOwner()

	if r.NotificationStore != nil && r.RunLockEnabled {
		acquired, lockErr := r.NotificationStore.AcquireRunLock(ctx, jobName, owner, r.RunLockTTL, time.Now())
		if lockErr != nil {
			return lockErr
		}
		if !acquired {
			r.Log.Warn().Str("job", jobName).Msg("attendance run skipped because another run lock is active")
			return nil
		}
		defer func() {
			if releaseErr := r.NotificationStore.ReleaseRunLock(context.Background(), jobName, owner); releaseErr != nil {
				r.Log.Warn().Err(releaseErr).Str("job", jobName).Msg("release run lock failed")
			}
		}()
	}

	stats := &runStats{}
	var runID int64
	if r.NotificationStore != nil {
		runID, err = r.NotificationStore.StartRun(ctx, notify.RunRecord{
			JobName:   jobName,
			Owner:     owner,
			StartedAt: time.Now(),
			Status:    "running",
		})
		if err != nil {
			return err
		}
		defer func() {
			status := "success"
			errText := ""
			if err != nil {
				status = "failed"
				errText = err.Error()
			}
			finishErr := r.NotificationStore.FinishRun(context.Background(), runID, notify.RunRecord{
				Status:              status,
				FinishedAt:          time.Now(),
				TotalCourses:        stats.TotalCourses,
				MatchedCourses:      stats.MatchedCourses,
				AttendanceFound:     stats.AttendanceFound,
				AttendanceSubmitted: stats.AttendanceSubmitted,
				AssignmentsFound:    stats.AssignmentsFound,
				QuizzesFound:        stats.QuizzesFound,
				NotificationsSent:   stats.NotificationsSent,
				RemindersSent:       stats.RemindersSent,
				DraftsReady:         stats.DraftsReady,
				Error:               errText,
			})
			if finishErr != nil {
				r.Log.Warn().Err(finishErr).Msg("finish run history failed")
			}
		}()
	}

	return r.runAttendance(ctx, stats)
}

func (r *Runner) runAttendance(ctx context.Context, stats *runStats) error {
	if err := r.M.Login(ctx, r.Username, r.Password); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	courses, err := r.M.GetCourses(ctx)

	if err != nil {
		return fmt.Errorf("courses: %w", err)
	}
	stats.TotalCourses = len(courses)

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
	stats.MatchedCourses = len(filteredCourses)

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

	if r.MaterialSyncEnabled && r.NotificationStore != nil {
		if err := r.syncCourseMaterials(ctx, filteredCourses); err != nil {
			r.Log.Warn().Err(err).Msg("course material sync pipeline")
		}
	}

	if err := r.fetchAssignmentsAndQuizzes(ctx, filteredCourses, stats); err != nil {
		r.Log.Warn().Err(err).Msg("assignment/quiz notification pipeline")
	}

	// Phase 2: detail page fetching
	if r.DetailFetchEnabled && r.NotificationStore != nil {
		if err := r.fetchDetailsForPendingItems(ctx); err != nil {
			r.Log.Warn().Err(err).Msg("detail page fetch pipeline")
		}
	}

	// Phase 3: deadline reminders
	if r.NotificationStore != nil {
		if err := r.checkDeadlineRemindersWithStats(ctx, now, stats); err != nil {
			r.Log.Warn().Err(err).Msg("deadline reminder pipeline")
		}
	}

	// Phase 4: LLM suggestions
	if r.SuggestionEnabled && r.LLMClient != nil && r.NotificationStore != nil {
		if err := r.processSuggestions(ctx); err != nil {
			r.Log.Warn().Err(err).Msg("suggestion pipeline")
		}
	}

	// Phase 5: AI draft solver
	if r.DraftEnabled && (r.MultiSolver != nil || r.Solver != nil) && r.NotificationStore != nil {
		if err := r.processDraftsWithStats(ctx, stats); err != nil {
			r.Log.Warn().Err(err).Msg("draft solver pipeline")
		}
	}

	if r.NotificationStore != nil {
		if err := r.processDraftReviews(ctx); err != nil {
			r.Log.Warn().Err(err).Msg("draft review pipeline")
		}
	}

	if r.PDFGenerationEnabled && r.NotificationStore != nil {
		if err := r.processPDFArtifacts(ctx); err != nil {
			r.Log.Warn().Err(err).Msg("pdf artifact pipeline")
		}
	}

	if r.NotificationStore != nil {
		if err := r.processApprovedSubmissions(ctx, now); err != nil {
			r.Log.Warn().Err(err).Msg("approved submission pipeline")
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
	stats.AttendanceFound = len(all)

	if len(all) == 0 {
		r.Log.Info().Msg("no attendance found")
		return nil
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(effectiveConcurrency(r.Conc))
	var submittedMu sync.Mutex
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
				submittedMu.Lock()
				stats.AttendanceSubmitted++
				submittedMu.Unlock()
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

func (r *Runner) fetchAssignmentsAndQuizzes(ctx context.Context, courses []moodle.Course, stats *runStats) error {
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
			dueDateParsed := parseDueDateRFC3339(a.DueDate)
			events = append(events, notify.NotificationEvent{
				EventType:            notify.NotificationTypeAssignment,
				CourseID:             c.CourseID,
				CourseName:           c.CourseName,
				ItemTitle:            a.Title,
				ItemName:             a.AssignmentName,
				ItemLink:             a.AssignmentLink,
				ItemID:               a.AssignmentID,
				DueDate:              a.DueDate,
				SubmissionStatus:     a.SubmissionStatus,
				Grade:                a.Grade,
				DueDateParsedRFC3339: dueDateParsed,
			})
			r.Log.Info().
				Str("course", a.Course.CourseName).
				Str("title", a.Title).
				Str("assignment", a.AssignmentName).
				Str("due_date", a.DueDate).
				Str("submission_status", a.SubmissionStatus).
				Str("grade", a.Grade).
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
			dueDateParsed := parseDueDateRFC3339(q.CloseDate)
			events = append(events, notify.NotificationEvent{
				EventType:            notify.NotificationTypeQuiz,
				CourseID:             c.CourseID,
				CourseName:           c.CourseName,
				ItemTitle:            q.Title,
				ItemName:             q.QuizName,
				ItemLink:             q.QuizLink,
				ItemID:               q.QuizID,
				DueDate:              q.CloseDate,
				Grade:                q.Grade,
				DueDateParsedRFC3339: dueDateParsed,
			})
			r.Log.Info().
				Str("course", q.Course.CourseName).
				Str("title", q.Title).
				Str("quiz", q.QuizName).
				Str("close_date", q.CloseDate).
				Str("grade", q.Grade).
				Str("link", q.QuizLink).
				Msg("quiz found")
		}
	}
	r.Log.Info().
		Int("total_assignments", totalAssignments).
		Int("total_quizzes", totalQuizzes).
		Msg("assignment and quiz scan completed")
	if stats != nil {
		stats.AssignmentsFound = totalAssignments
		stats.QuizzesFound = totalQuizzes
	}

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

		if err := r.sendReliable(targets); err != nil {
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
		if stats != nil {
			stats.NotificationsSent++
		}
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
	return r.messageTargets(message)
}

func (r *Runner) messageTargets(message string) []notify.GroupMessage {
	targets := make([]notify.GroupMessage, 0, 2)
	if strings.TrimSpace(r.WAMe) != "" {
		targets = append(targets, notify.GroupMessage{Message: message, GroupID: r.WAMe})
	}
	if strings.TrimSpace(r.WAGroup) != "" {
		targets = append(targets, notify.GroupMessage{Message: "🤖 " + message, GroupID: r.WAGroup})
	}
	return targets
}

func (r *Runner) sendReliable(msgs []notify.GroupMessage) error {
	if r.sendWhatsApp != nil {
		return r.sendWhatsApp(msgs)
	}
	return notify.SendWhatsAppReliable(msgs)
}

func buildNotificationMessage(item notify.PendingNotification) string {
	label := notificationLabel(item.EventType)
	title := strings.TrimSpace(item.ItemTitle)
	if title == "" {
		title = "-"
	}
	dueDate := formatWIB(item.DueDateParsed)
	if dueDate == "-" {
		dueDate = strings.TrimSpace(item.DueDate)
		if dueDate == "" {
			dueDate = "-"
		}
	}
	status := strings.TrimSpace(item.SubmissionStatus)
	if status == "" {
		status = "-"
	}
	grade := strings.TrimSpace(item.Grade)
	if grade == "" {
		grade = "-"
	}
	return fmt.Sprintf(
		"📚 %s baru terdeteksi!\n\nMata Kuliah: %s\nTopik: %s\nItem: %s\nDue: %s\nStatus: %s\nGrade: %s\nLink: %s",
		label,
		item.CourseName,
		title,
		item.ItemName,
		dueDate,
		status,
		grade,
		item.ItemLink,
	)
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

func runOwner() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

func parseDueDateRFC3339(raw string) string {
	t, err := moodle.ParseMoodleDate(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// formatWIB formats an RFC3339 timestamp as human-readable WIB (UTC+7).
// Falls back to the raw string if parsing fails.
func formatWIB(rfcStr string) string {
	if rfcStr == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, rfcStr)
	if err != nil {
		return rfcStr
	}
	wib := time.FixedZone("WIB", 7*3600)
	return t.In(wib).Format("02 Jan 2006, 15:04 WIB")
}
