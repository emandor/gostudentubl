package moodle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Client struct {
	HC   *http.Client
	Base struct {
		LoginURL          string
		CoursesURL        string
		AttendanceListURL string
		AttendanceURL     string
		AttendanceFormURL string
	}
	UA string
}

type ViewInfo struct{ SessionID, SessKey, SubmitLink string }

func (c *Client) ViewAttendanceByID(ctx context.Context, attendanceID string) (ViewInfo, error) {
	u := fmt.Sprintf("%s?id=%s", c.Base.AttendanceURL, attendanceID)
	doc, _, err := c.get(ctx, u)
	if err != nil {
		return ViewInfo{}, err
	}
	return parseViewInfo(doc)
}

func (c *Client) get(ctx context.Context, u string) (*goquery.Document, *http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", c.UA)
	res, err := c.HC.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { /* no body close here, goquery will */ }()
	doc, err := goquery.NewDocumentFromReader(res.Body)
	return doc, res, err
}

func (c *Client) postForm(ctx context.Context, u string, data url.Values) (*goquery.Document, *http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.UA)
	res, err := c.HC.Do(req)
	if err != nil {
		return nil, nil, err
	}
	doc, err := goquery.NewDocumentFromReader(res.Body)
	return doc, res, err
}

func (c *Client) Login(ctx context.Context, username, password string) error {
	// 1) fetch login page for logintoken
	doc, _, err := c.get(ctx, c.Base.LoginURL)
	if err != nil {
		return err
	}
	logintoken, exists := doc.Find(`input[name="logintoken"]`).Attr("value")
	if !exists || logintoken == "" {
		return errors.New("missing login token")
	}
	// 2) submit login form; cookies/redirect handled by client+jar
	form := url.Values{
		"username":   {username},
		"password":   {password},
		"logintoken": {logintoken},
	}
	_, resp, err := c.postForm(ctx, c.Base.LoginURL, form)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("login http status %d", resp.StatusCode)
	}
	// 3) sanity check: courses page must not contain login form
	courses, _, err := c.get(ctx, c.Base.CoursesURL)
	if err != nil {
		return err
	}
	if courses.Find(`#username`).Length() > 0 {
		return errors.New("login failed: username field still present")
	}
	return nil
}

type Course struct {
	CourseName string
	CourseLink string
	CourseID   int
	Grade      *int
	Periode    string // MMYY
	Group      string // e.g. A1
}

type Attendance struct {
	Title          string
	AttendanceName string
	AttendanceLink string
	AttendanceID   string
	Course         Course
}

// GetCourses parses the overview table similar to the TS version.
func (c *Client) GetCourses(ctx context.Context) ([]Course, error) {
	doc, _, err := c.get(ctx, c.Base.CoursesURL)
	if err != nil {
		return nil, err
	}
	return parseCourses(doc)
}

func (c *Client) GetAttendance(ctx context.Context, cr Course) ([]Attendance, error) {
	courseID := fmt.Sprintf("%d", cr.CourseID)
	u := fmt.Sprintf("%s?id=%s", c.Base.AttendanceListURL, courseID)
	doc, _, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	return parseAttendanceList(doc, cr), nil
}

type FormInfo struct {
	SessID  string
	SessKey string
	QF      string
	IsExp   string
	Status  string
}

func (c *Client) GetFormInfo(ctx context.Context, submitLink, wantSessID, wantSessKey string) (FormInfo, error) {
	doc, _, err := c.get(ctx, submitLink)
	if err != nil {
		return FormInfo{}, err
	}
	fi := parseFormInfo(doc)
	if fi.SessID != wantSessID || fi.SessKey != wantSessKey {
		return FormInfo{}, errors.New("form sess mismatch")
	}
	return fi, nil
}

func (c *Client) SubmitAttendance(ctx context.Context, formURL string, fi FormInfo) error {
	data := url.Values{
		"sessid":  {fi.SessID},
		"sesskey": {fi.SessKey},
		"_qf__mod_attendance_form_studentattendance": {fi.QF},
		"mform_isexpanded_id_session":                {fi.IsExp},
		"status":                                     {fi.Status},
		"submitbutton":                               {"Save changes"},
	}
	_, _, err := c.postForm(ctx, c.Base.AttendanceFormURL, data)
	return err
}

func (c *Client) CheckSubmitted(ctx context.Context, attendanceID string) (bool, error) {
	u := fmt.Sprintf("%s?id=%s", c.Base.AttendanceURL, attendanceID)
	doc, _, err := c.get(ctx, u)
	if err != nil {
		return false, err
	}
	return doc.Find("td:contains('Self-recorded')").Length() > 0, nil
}
