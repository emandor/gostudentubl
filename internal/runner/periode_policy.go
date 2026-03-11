package runner

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var errInvalidPeriodePolicy = errors.New("invalid periode policy configuration")

func nowInTimezone(tz string, now time.Time) (time.Time, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return now, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return now, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return now.In(loc), nil
}

func resolveAllowedPeriodes(mode, allowedRaw, legacyCurrent string, now time.Time) ([]string, error) {
	mode = normalizePeriodeMode(mode)
	legacyCurrent = normalizePeriode(legacyCurrent)

	switch mode {
	case "manual":
		manual := normalizePeriodeList(allowedRaw)
		if len(manual) > 0 {
			return manual, nil
		}
		if legacyCurrent != "" {
			return []string{legacyCurrent}, nil
		}
		return nil, fmt.Errorf("%w: PERIODE_MODE=manual requires ALLOWED_PERIODES or CURRENT_PERIODE", errInvalidPeriodePolicy)
	case "legacy":
		if legacyCurrent != "" {
			return []string{legacyCurrent}, nil
		}
		return nil, fmt.Errorf("%w: PERIODE_MODE=legacy requires CURRENT_PERIODE", errInvalidPeriodePolicy)
	case "", "auto":
		return autoPeriodes(now), nil
	default:
		return nil, fmt.Errorf("%w: unsupported PERIODE_MODE %q", errInvalidPeriodePolicy, mode)
	}
}

func normalizePeriodeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "auto"
	}
	return mode
}

func autoPeriodes(now time.Time) []string {
	current := formatPeriode(now)
	next := formatPeriode(now.AddDate(0, 1, 0))
	if current == next {
		return []string{current}
	}
	return []string{current, next}
}

func formatPeriode(t time.Time) string {
	return fmt.Sprintf("%02d%02d", int(t.Month()), t.Year()%100)
}

func normalizePeriodeList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', '\t', '\n', '\r', ' ':
			return true
		default:
			return false
		}
	})

	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		n := normalizePeriode(p)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func normalizePeriode(v string) string {
	v = strings.TrimSpace(v)
	if !isValidPeriode(v) {
		return ""
	}
	return v
}

func isValidPeriode(v string) bool {
	if len(v) != 4 {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	mm, err := strconv.Atoi(v[:2])
	if err != nil {
		return false
	}
	return mm >= 1 && mm <= 12
}

func isAllowedPeriode(periode string, allowedSet map[string]struct{}) bool {
	_, ok := allowedSet[normalizePeriode(periode)]
	return ok
}
