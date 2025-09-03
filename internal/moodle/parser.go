package moodle

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

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
