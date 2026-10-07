// Package worktime parses the durations and dates of YouTrack work items.
//
// It mirrors workit's YouTrack time tracking (packages/workit-core
// youtrack.ts: youTrackParseDuration and youTrackWorkDateMs) so workit can
// hand its input to ytrack unchanged. Where workit silently drops part of
// an input ("1h30" is 60 minutes, "1.5h" is 300), ytrack refuses it instead.
package worktime

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	bareMinutes = regexp.MustCompile(`^\d+$`)
	// term is one "<number><unit>" part; whitespace may separate the number,
	// the unit and the next part.
	term = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([a-z]+)\s*`)
)

var unitMinutes = map[string]float64{
	"h": 60, "hr": 60, "hrs": 60, "hour": 60, "hours": 60,
	"m": 1, "min": 1, "mins": 1, "minute": 1, "minutes": 1,
}

// ParseDuration returns the minutes in a duration such as 1h30m, 90m,
// 1.5h, "1 hour 30 minutes" or a bare number of minutes (90). Parts add up
// and case is ignored, as in workit. The result must be a positive whole
// number of minutes. Days and weeks are refused: their length depends on
// the YouTrack instance's work schedule.
func ParseDuration(s string) (int, error) {
	text := strings.ToLower(strings.TrimSpace(s))
	if text == "" {
		return 0, fmt.Errorf("empty duration; use e.g. 1h30m, 90m or 1.5h")
	}
	if bareMinutes.MatchString(text) {
		n, err := strconv.Atoi(text)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid duration %q: it must be more than 0 minutes", s)
		}
		return n, nil
	}
	total := 0.0
	for rest := text; rest != ""; {
		m := term.FindStringSubmatch(rest)
		if m == nil {
			return 0, fmt.Errorf("invalid duration %q: use hours and minutes, e.g. 1h30m, 90m or 1.5h", s)
		}
		factor, ok := unitMinutes[m[2]]
		if !ok {
			if m[2] == "d" || m[2] == "w" || strings.HasPrefix(m[2], "day") || strings.HasPrefix(m[2], "week") {
				return 0, fmt.Errorf("invalid duration %q: days and weeks depend on the YouTrack work schedule; give hours or minutes", s)
			}
			return 0, fmt.Errorf("invalid duration %q: unknown unit %q (use h or m)", s, m[2])
		}
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		total += n * factor
		rest = rest[len(m[0]):]
	}
	minutes := math.Round(total)
	if math.Abs(total-minutes) > 1e-9 {
		return 0, fmt.Errorf("invalid duration %q: it is not a whole number of minutes", s)
	}
	if minutes <= 0 {
		return 0, fmt.Errorf("invalid duration %q: it must be more than 0 minutes", s)
	}
	if minutes > math.MaxInt32 {
		return 0, fmt.Errorf("invalid duration %q: too long", s)
	}
	return int(minutes), nil
}

// FormatMinutes renders minutes as YouTrack shows durations without a work
// schedule: "1h 30m", "45m", "2h".
func FormatMinutes(minutes int) string {
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// DateLayout is the layout of work item dates on the command line and in
// output.
const DateLayout = "2006-01-02"

// WorkDate resolves a work item date: "auto" (or "") is today's calendar day
// in loc at the instant now; otherwise raw is a YYYY-MM-DD day (one-digit
// months and days are accepted, as in workit). The result is that day at
// midnight UTC, which is how YouTrack stores a work item's date, so a given
// day is the same instant whatever timezone ytrack runs in.
func WorkDate(raw string, now time.Time, loc *time.Location) (time.Time, error) {
	if raw == "" || raw == "auto" {
		d := now.In(loc)
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	d, err := ParseDay(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: use auto or YYYY-MM-DD", raw)
	}
	return d, nil
}

// ParseDay parses a YYYY-MM-DD day as midnight UTC. It rejects days that do
// not exist, such as 2026-02-30.
func ParseDay(raw string) (time.Time, error) {
	return time.Parse("2006-1-2", strings.TrimSpace(raw))
}

// Location returns the timezone that decides today's date for "auto": name
// when set (an IANA zone such as Europe/Madrid), otherwise fallback, the
// process timezone (which honours TZ). It mirrors workit, where the
// youtrack.json timezone wins over the process timezone.
func Location(name string, fallback *time.Location) (*time.Location, error) {
	if strings.TrimSpace(name) == "" {
		return fallback, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(name))
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q in config.yml: %w", name, err)
	}
	return loc, nil
}
