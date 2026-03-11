package moodle

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	rexCourseID   = regexp.MustCompile(`id=(\d+)`)
	rexPeriode    = regexp.MustCompile(`-(\d{4})-`)
	rexGroupTrail = regexp.MustCompile(`-(\w{1}\d{1})`)
)

func parseCourses(doc *goquery.Document) ([]Course, error) {
	var cs []Course
	doc.Find("#overview-grade tbody tr").Each(func(i int, s *goquery.Selection) {
		anchor := s.Find("td.cell.c0 a")
		nameRaw := strings.TrimSpace(anchor.Text())
		if nameRaw == "" {
			return
		}
		courseName := rexGroupTrail.ReplaceAllString(strings.Split(nameRaw, " (")[0], "")
		link, _ := anchor.Attr("href")
		if link == "" {
			return
		}

		var grade *int
		if g, err := strconv.Atoi(strings.TrimSpace(s.Find("td.cell.c1").Text())); err == nil {
			grade = &g
		}
		var cid int
		if m := rexCourseID.FindStringSubmatch(link); len(m) == 2 {
			cid, _ = strconv.Atoi(m[1])
		}
		if cid == 0 {
			return
		}
		periode := ""
		if m := rexPeriode.FindStringSubmatch(nameRaw); len(m) == 2 {
			periode = m[1]
		}
		group := ""

		if m := rexGroupTrail.FindStringSubmatch(nameRaw); len(m) == 2 {
			group = m[1]
		}
		if periode == "" || group == "" {
			return
		}
		cs = append(cs, Course{CourseName: courseName, CourseLink: link, CourseID: cid, Grade: grade, Periode: periode, Group: group})
	})
	return cs, nil
}

func parseAttendanceList(doc *goquery.Document, cr Course) []Attendance {
	if strings.TrimSpace(doc.Find("#notice").Text()) == "There are no Attendance in this course" {
		return nil
	}
	var out []Attendance
	doc.Find(".generaltable tbody tr").Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find("td.cell.c0").Text())
		nameEl := s.Find("td.cell.c1 a")
		name := strings.TrimSpace(nameEl.Text())
		link, _ := nameEl.Attr("href")
		if title == "" || name == "" || link == "" {
			return
		}
		attID := ""
		if m := rexCourseID.FindStringSubmatch(link); len(m) == 2 {
			attID = m[1]
		}
		if attID == "" {
			return
		}
		out = append(out, Attendance{Title: title, AttendanceName: name, AttendanceLink: link, AttendanceID: attID, Course: cr})
	})
	return out
}

func parseAssignmentList(doc *goquery.Document, cr Course) []Assignment {
	notice := strings.ToLower(strings.TrimSpace(doc.Find("#notice").Text()))
	if strings.Contains(notice, "no assignments") {
		return nil
	}
	var out []Assignment
	doc.Find(".generaltable tbody tr").Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find("td.cell.c0").Text())
		nameEl := s.Find("td.cell.c1 a")
		if nameEl.Length() == 0 {
			nameEl = s.Find("td.c1 a")
		}
		name := strings.TrimSpace(nameEl.Text())
		link, _ := nameEl.Attr("href")
		if title == "" || name == "" || link == "" {
			return
		}
		assignID := ""
		if m := rexCourseID.FindStringSubmatch(link); len(m) == 2 {
			assignID = m[1]
		}
		if assignID == "" {
			return
		}
		dueDate := strings.TrimSpace(s.Find("td.cell.c2").Text())
		submissionStatus := strings.TrimSpace(s.Find("td.cell.c3").Text())
		grade := strings.TrimSpace(s.Find("td.cell.c4").Text())

		out = append(out, Assignment{
			Title:            title,
			AssignmentName:   name,
			AssignmentLink:   link,
			AssignmentID:     assignID,
			DueDate:          dueDate,
			SubmissionStatus: submissionStatus,
			Grade:            grade,
			Course:           cr,
		})
	})
	return out
}

func parseQuizList(doc *goquery.Document, cr Course) []Quiz {
	notice := strings.ToLower(strings.TrimSpace(doc.Find("#notice").Text()))
	if strings.Contains(notice, "no quizzes") {
		return nil
	}
	var out []Quiz
	doc.Find(".generaltable tbody tr").Each(func(i int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find("td.cell.c0").Text())
		nameEl := s.Find("td.cell.c1 a")
		if nameEl.Length() == 0 {
			nameEl = s.Find("td.c1 a")
		}
		name := strings.TrimSpace(nameEl.Text())
		link, _ := nameEl.Attr("href")
		if title == "" || name == "" || link == "" {
			return
		}
		quizID := ""
		if m := rexCourseID.FindStringSubmatch(link); len(m) == 2 {
			quizID = m[1]
		}
		if quizID == "" {
			return
		}
		closeDate := strings.TrimSpace(s.Find("td.cell.c2").Text())
		grade := strings.TrimSpace(s.Find("td.cell.c3").Text())

		out = append(out, Quiz{
			Title:     title,
			QuizName:  name,
			QuizLink:  link,
			QuizID:    quizID,
			CloseDate: closeDate,
			Grade:     grade,
			Course:    cr,
		})
	})
	return out
}

func parseViewInfo(doc *goquery.Document) (ViewInfo, error) {
	var vi ViewInfo
	doc.Find("a").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if strings.Contains(strings.TrimSpace(s.Text()), "Submit attendance") {
			link, _ := s.Attr("href")
			if link == "" {
				return true
			}
			vi.SubmitLink = link
			vi.SessionID = firstMatch(link, `sessid=(\d+)`)
			vi.SessKey = firstMatch(link, `sesskey=(\w+)`)
			return false
		}
		return true
	})
	if vi.SubmitLink == "" || vi.SessionID == "" || vi.SessKey == "" {
		return vi, fmt.Errorf("view info not found")
	}
	return vi, nil
}

func parseFormInfo(doc *goquery.Document) FormInfo {
	val := func(name string) string { v, _ := doc.Find("input[name='" + name + "']").Attr("value"); return v }
	return FormInfo{
		SessID:  val("sessid"),
		SessKey: val("sesskey"),
		QF:      val("_qf__mod_attendance_form_studentattendance"),
		IsExp:   val("mform_isexpanded_id_session"),
		Status:  val("status"),
	}
}

func firstMatch(s, pattern string) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(s)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// parseMoodleDate parses "Saturday, 28 February 2026, 11:59 PM" → time.Time
// Moodle format uses Go reference: "Monday, 2 January 2006, 3:04 PM"
func ParseMoodleDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}
	// Moodle date format
	const layout = "Monday, 2 January 2006, 3:04 PM"
	t, err := time.Parse(layout, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse moodle date %q: %w", raw, err)
	}
	return t, nil
}

func parseAssignmentDetail(doc *goquery.Document) AssignmentDetail {
	var ad AssignmentDetail
	doc.Find(".submissionsummarytable tr").Each(func(i int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Find("td.cell.c0").Text())
		value := strings.TrimSpace(s.Find("td.cell.c1").Text())
		switch {
		case strings.Contains(label, "Submission status"):
			ad.SubmissionStatus = value
		case strings.Contains(label, "Grading status"):
			ad.GradingStatus = value
		case strings.Contains(label, "Due date"):
			ad.DueDate = value
		case strings.Contains(label, "Time remaining"):
			ad.TimeRemaining = value
		case strings.Contains(label, "Last modified"):
			ad.LastModified = value
		case strings.Contains(label, "File submissions"):
			s.Find("td.cell.c1 a").Each(func(j int, a *goquery.Selection) {
				name := strings.TrimSpace(a.Text())
				if name != "" {
					ad.FileSubmissions = append(ad.FileSubmissions, name)
				}
			})
		}
	})
	ad.HasEditButton = doc.Find("input[value='Edit submission'], a:contains('Edit submission')").Length() > 0
	if ad.FileSubmissions == nil {
		ad.FileSubmissions = []string{}
	}
	return ad
}

func parseQuizDetail(doc *goquery.Document) QuizDetail {
	var qd QuizDetail
	// Parse quiz info box
	doc.Find(".quizinfo p, .quizinfo li").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		switch {
		case strings.Contains(text, "Attempts allowed"):
			qd.AttemptsAllowed = strings.TrimSpace(strings.SplitN(text, ":", 2)[1])
		case strings.Contains(text, "close"):
			parts := strings.SplitN(text, ":", 2)
			if len(parts) == 2 {
				qd.CloseDate = strings.TrimSpace(parts[1])
			}
		}
	})
	// Parse attempts summary table
	doc.Find(".quizattemptsummary tbody tr").Each(func(i int, s *goquery.Selection) {
		state := strings.TrimSpace(s.Find("td.cell.c1").Text())
		if state != "" {
			qd.AttemptState = state
		}
		grade := strings.TrimSpace(s.Find("td.cell.c2").Text())
		if grade != "" {
			qd.AttemptGrade = grade
		}
		date := strings.TrimSpace(s.Find("td.cell.c3").Text())
		if date != "" {
			qd.AttemptDate = date
		}
	})
	// Check buttons/messages
	qd.CanAttempt = doc.Find("input[value='Attempt quiz now'], input[value='Attempt quiz'], a:contains('Attempt quiz now'), button:contains('Attempt quiz')").Length() > 0
	qd.NoMoreAttempts = strings.Contains(doc.Text(), "No more attempts") || strings.Contains(doc.Text(), "no more attempts")
	return qd
}
