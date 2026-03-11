package runner

import (
	"testing"
	"time"
)

func TestNowInTimezoneValid(t *testing.T) {
	now := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	got, err := nowInTimezone("Asia/Jakarta", now)
	if err != nil {
		t.Fatalf("unexpected error for valid timezone: %v", err)
	}
	if got.Location().String() != "Asia/Jakarta" {
		t.Fatalf("expected Asia/Jakarta, got %s", got.Location())
	}
}

func TestNowInTimezoneInvalidErrors(t *testing.T) {
	now := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	_, err := nowInTimezone("Not/ATimezone", now)
	if err == nil {
		t.Fatal("expected error for invalid timezone, got nil")
	}
}

func TestNowInTimezoneEmptyUsesInput(t *testing.T) {
	now := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	got, err := nowInTimezone("", now)
	if err != nil {
		t.Fatalf("unexpected error for empty timezone: %v", err)
	}
	if !got.Equal(now) {
		t.Fatalf("expected input time back, got %v", got)
	}
}

func TestResolveAllowedPeriodesAuto(t *testing.T) {
	now := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	allowed, err := resolveAllowedPeriodes("auto", "", "", now)
	if err != nil {
		t.Fatalf("unexpected error in auto mode: %v", err)
	}
	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed periodes, got %d (%v)", len(allowed), allowed)
	}

	if allowed[0] != "0226" || allowed[1] != "0326" {
		t.Fatalf("unexpected auto periodes: %v", allowed)
	}
}

func TestResolveAllowedPeriodesYearBoundary(t *testing.T) {
	now := time.Date(2025, 12, 15, 10, 0, 0, 0, time.UTC)
	allowed, err := resolveAllowedPeriodes("auto", "", "", now)
	if err != nil {
		t.Fatalf("unexpected error in auto mode: %v", err)
	}
	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed periodes, got %d (%v)", len(allowed), allowed)
	}

	if allowed[0] != "1225" || allowed[1] != "0126" {
		t.Fatalf("unexpected year-boundary periodes: %v", allowed)
	}
}

func TestResolveAllowedPeriodesManual(t *testing.T) {
	now := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	allowed, err := resolveAllowedPeriodes("manual", "0226, 0326;0326", "", now)
	if err != nil {
		t.Fatalf("unexpected error in manual mode: %v", err)
	}
	if len(allowed) != 2 {
		t.Fatalf("expected 2 unique manual periodes, got %d (%v)", len(allowed), allowed)
	}
	if allowed[0] != "0226" || allowed[1] != "0326" {
		t.Fatalf("unexpected manual periodes: %v", allowed)
	}
}

func TestResolveAllowedPeriodesLegacyFallback(t *testing.T) {
	now := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	allowed, err := resolveAllowedPeriodes("manual", "", "0226", now)
	if err != nil {
		t.Fatalf("unexpected error in manual fallback mode: %v", err)
	}
	if len(allowed) != 1 || allowed[0] != "0226" {
		t.Fatalf("expected legacy fallback periode [0226], got %v", allowed)
	}
}

func TestResolveAllowedPeriodesLegacyMode(t *testing.T) {
	now := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)

	allowed, err := resolveAllowedPeriodes("legacy", "", "1125", now)
	if err != nil {
		t.Fatalf("unexpected error in legacy mode: %v", err)
	}
	if len(allowed) != 1 || allowed[0] != "1125" {
		t.Fatalf("expected legacy periode [1125], got %v", allowed)
	}
}

func TestResolveAllowedPeriodesUnknownModeErrors(t *testing.T) {
	now := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)

	_, err := resolveAllowedPeriodes("AUTOX", "", "", now)
	if err == nil {
		t.Fatalf("expected error for unsupported periode mode")
	}
}

func TestResolveAllowedPeriodesManualEmptyErrors(t *testing.T) {
	now := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)

	_, err := resolveAllowedPeriodes("manual", "", "", now)
	if err == nil {
		t.Fatalf("expected error for manual mode without periode values")
	}
}

func TestResolveAllowedPeriodesLegacyEmptyErrors(t *testing.T) {
	now := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)

	_, err := resolveAllowedPeriodes("legacy", "", "", now)
	if err == nil {
		t.Fatalf("expected error for legacy mode without CURRENT_PERIODE")
	}
}

func TestIsAllowedPeriode(t *testing.T) {
	allowed := map[string]struct{}{"0226": {}, "0326": {}}

	cases := []struct {
		periode string
		want    bool
	}{
		{periode: "0226", want: true},
		{periode: "0326", want: true},
		{periode: "0126", want: false},
		{periode: "1326", want: false},
		{periode: "", want: false},
	}

	for _, tc := range cases {
		got := isAllowedPeriode(tc.periode, allowed)
		if got != tc.want {
			t.Fatalf("isAllowedPeriode(%q) = %v, want %v", tc.periode, got, tc.want)
		}
	}
}

func TestEffectiveConcurrency(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{in: -1, want: 1},
		{in: 0, want: 1},
		{in: 1, want: 1},
		{in: 4, want: 4},
	}

	for _, tc := range cases {
		got := effectiveConcurrency(tc.in)
		if got != tc.want {
			t.Fatalf("effectiveConcurrency(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestExceedsMaxCourses(t *testing.T) {
	cases := []struct {
		max     int
		matched int
		want    bool
	}{
		{max: 0, matched: 100, want: false},
		{max: -1, matched: 2, want: false},
		{max: 2, matched: 2, want: false},
		{max: 2, matched: 3, want: true},
	}

	for _, tc := range cases {
		got := exceedsMaxCourses(tc.max, tc.matched)
		if got != tc.want {
			t.Fatalf("exceedsMaxCourses(%d, %d) = %v, want %v", tc.max, tc.matched, got, tc.want)
		}
	}
}
