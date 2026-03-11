package moodle

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestParseMoodleDateValid(t *testing.T) {
	tests := []struct {
		input string
		year  int
		month time.Month
		day   int
		hour  int
		min   int
	}{
		{"Saturday, 28 February 2026, 11:59 PM", 2026, time.February, 28, 23, 59},
		{"Monday, 30 March 2026, 11:59 PM", 2026, time.March, 30, 23, 59},
		{"Sunday, 1 January 2026, 8:00 AM", 2026, time.January, 1, 8, 0},
		{"Friday, 15 May 2026, 3:30 PM", 2026, time.May, 15, 15, 30},
	}
	for _, tc := range tests {
		got, err := ParseMoodleDate(tc.input)
		if err != nil {
			t.Fatalf("ParseMoodleDate(%q) error: %v", tc.input, err)
		}
		if got.Year() != tc.year || got.Month() != tc.month || got.Day() != tc.day || got.Hour() != tc.hour || got.Minute() != tc.min {
			t.Fatalf("ParseMoodleDate(%q) = %v, want %d-%02d-%02d %02d:%02d", tc.input, got, tc.year, tc.month, tc.day, tc.hour, tc.min)
		}
	}
}

func TestParseMoodleDateInvalid(t *testing.T) {
	tests := []string{"", "  ", "not a date", "2026-02-28", "28/02/2026"}
	for _, input := range tests {
		_, err := ParseMoodleDate(input)
		if err == nil {
			t.Fatalf("ParseMoodleDate(%q) expected error, got nil", input)
		}
	}
}

const assignmentListHTML = `
<html><body>
<table class="generaltable">
<tbody>
<tr>
  <td class="cell c0">Topik 1</td>
  <td class="cell c1"><a href="https://elearning.example.com/mod/assign/view.php?id=1001">Assignment 1</a></td>
  <td class="cell c2">Saturday, 28 February 2026, 11:59 PM</td>
  <td class="cell c3">Submitted for grading</td>
  <td class="cell c4">85</td>
</tr>
<tr>
  <td class="cell c0">Topik 2</td>
  <td class="cell c1"><a href="https://elearning.example.com/mod/assign/view.php?id=1002">Assignment 2</a></td>
  <td class="cell c2">Monday, 30 March 2026, 11:59 PM</td>
  <td class="cell c3">No submission</td>
  <td class="cell c4">-</td>
</tr>
</tbody>
</table>
</body></html>
`

func TestParseAssignmentListWithNewFields(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(assignmentListHTML))
	if err != nil {
		t.Fatal(err)
	}
	cr := Course{CourseName: "Test", CourseID: 1}
	result := parseAssignmentList(doc, cr)

	if len(result) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(result))
	}

	a1 := result[0]
	if a1.DueDate != "Saturday, 28 February 2026, 11:59 PM" {
		t.Fatalf("expected DueDate for a1, got %q", a1.DueDate)
	}
	if a1.SubmissionStatus != "Submitted for grading" {
		t.Fatalf("expected SubmissionStatus for a1, got %q", a1.SubmissionStatus)
	}
	if a1.Grade != "85" {
		t.Fatalf("expected Grade '85' for a1, got %q", a1.Grade)
	}

	a2 := result[1]
	if a2.DueDate != "Monday, 30 March 2026, 11:59 PM" {
		t.Fatalf("expected DueDate for a2, got %q", a2.DueDate)
	}
	if a2.SubmissionStatus != "No submission" {
		t.Fatalf("expected 'No submission' for a2, got %q", a2.SubmissionStatus)
	}
	if a2.Grade != "-" {
		t.Fatalf("expected Grade '-' for a2, got %q", a2.Grade)
	}
}

const quizListHTML = `
<html><body>
<table class="generaltable">
<tbody>
<tr>
  <td class="cell c0">Topik 1</td>
  <td class="cell c1"><a href="https://elearning.example.com/mod/quiz/view.php?id=2001">Quiz 1</a></td>
  <td class="cell c2">Monday, 30 March 2026, 11:59 PM</td>
  <td class="cell c3">100.00/100.00</td>
</tr>
<tr>
  <td class="cell c0">Topik 2</td>
  <td class="cell c1"><a href="https://elearning.example.com/mod/quiz/view.php?id=2002">Quiz 2</a></td>
  <td class="cell c2">Friday, 10 April 2026, 11:59 PM</td>
  <td class="cell c3"></td>
</tr>
</tbody>
</table>
</body></html>
`

func TestParseQuizListWithNewFields(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(quizListHTML))
	if err != nil {
		t.Fatal(err)
	}
	cr := Course{CourseName: "Test", CourseID: 1}
	result := parseQuizList(doc, cr)

	if len(result) != 2 {
		t.Fatalf("expected 2 quizzes, got %d", len(result))
	}

	q1 := result[0]
	if q1.CloseDate != "Monday, 30 March 2026, 11:59 PM" {
		t.Fatalf("expected CloseDate for q1, got %q", q1.CloseDate)
	}
	if q1.Grade != "100.00/100.00" {
		t.Fatalf("expected Grade '100.00/100.00' for q1, got %q", q1.Grade)
	}

	q2 := result[1]
	if q2.CloseDate != "Friday, 10 April 2026, 11:59 PM" {
		t.Fatalf("expected CloseDate for q2, got %q", q2.CloseDate)
	}
	if q2.Grade != "" {
		t.Fatalf("expected empty Grade for q2, got %q", q2.Grade)
	}
}

const assignmentDetailHTML = `
<html><body>
<table class="submissionsummarytable">
<tr>
  <td class="cell c0">Submission status</td>
  <td class="cell c1">Submitted for grading</td>
</tr>
<tr>
  <td class="cell c0">Grading status</td>
  <td class="cell c1">Graded</td>
</tr>
<tr>
  <td class="cell c0">Due date</td>
  <td class="cell c1">Saturday, 28 February 2026, 11:59 PM</td>
</tr>
<tr>
  <td class="cell c0">Time remaining</td>
  <td class="cell c1">Assignment was submitted 2 days early</td>
</tr>
<tr>
  <td class="cell c0">File submissions</td>
  <td class="cell c1"><a href="#">homework.pdf</a></td>
</tr>
</table>
</body></html>
`

func TestParseAssignmentDetail(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(assignmentDetailHTML))
	if err != nil {
		t.Fatal(err)
	}
	ad := parseAssignmentDetail(doc)

	if ad.SubmissionStatus != "Submitted for grading" {
		t.Fatalf("expected 'Submitted for grading', got %q", ad.SubmissionStatus)
	}
	if ad.GradingStatus != "Graded" {
		t.Fatalf("expected 'Graded', got %q", ad.GradingStatus)
	}
	if ad.DueDate != "Saturday, 28 February 2026, 11:59 PM" {
		t.Fatalf("expected due date, got %q", ad.DueDate)
	}
	if len(ad.FileSubmissions) != 1 || ad.FileSubmissions[0] != "homework.pdf" {
		t.Fatalf("expected 1 file submission 'homework.pdf', got %v", ad.FileSubmissions)
	}
}

const quizDetailHTML = `
<html><body>
<div class="quizinfo">
  <p>Attempts allowed: 1</p>
  <p>This quiz close: Monday, 30 March 2026, 11:59 PM</p>
</div>
<table class="quizattemptsummary">
<tbody>
<tr>
  <td class="cell c0">1</td>
  <td class="cell c1">Finished</td>
  <td class="cell c2">95.00</td>
  <td class="cell c3">Sunday, 29 March 2026, 10:00 PM</td>
</tr>
</tbody>
</table>
<p>No more attempts</p>
</body></html>
`

func TestParseQuizDetail(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(quizDetailHTML))
	if err != nil {
		t.Fatal(err)
	}
	qd := parseQuizDetail(doc)

	if qd.AttemptsAllowed != "1" {
		t.Fatalf("expected AttemptsAllowed '1', got %q", qd.AttemptsAllowed)
	}
	if qd.AttemptState != "Finished" {
		t.Fatalf("expected AttemptState 'Finished', got %q", qd.AttemptState)
	}
	if qd.AttemptGrade != "95.00" {
		t.Fatalf("expected AttemptGrade '95.00', got %q", qd.AttemptGrade)
	}
	if !qd.NoMoreAttempts {
		t.Fatal("expected NoMoreAttempts=true")
	}
	if qd.CanAttempt {
		t.Fatal("expected CanAttempt=false")
	}
}

func TestParseQuizDetailWithAttemptButton(t *testing.T) {
	html := `<html><body>
<div class="quizinfo"><p>Attempts allowed: Unlimited</p></div>
<input type="submit" value="Attempt quiz now">
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	qd := parseQuizDetail(doc)
	if !qd.CanAttempt {
		t.Fatal("expected CanAttempt=true")
	}
	if qd.AttemptsAllowed != "Unlimited" {
		t.Fatalf("expected 'Unlimited', got %q", qd.AttemptsAllowed)
	}
}
