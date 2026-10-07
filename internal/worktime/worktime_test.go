package worktime

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// workitParseDuration is workit's youTrackParseDuration
// (workit-core/src/core/youtrack.ts), ported line by line so the table below
// can prove which inputs ytrack reads exactly as workit does. It returns 0
// where workit returns "could not parse duration".
func workitParseDuration(text string) int {
	lower := strings.TrimSpace(strings.ToLower(text))
	total := 0
	for _, m := range regexp.MustCompile(`(\d+)\s*h`).FindAllStringSubmatch(lower, -1) {
		n, _ := strconv.Atoi(m[1])
		total += n * 60
	}
	for _, m := range regexp.MustCompile(`(\d+)\s*m`).FindAllStringSubmatch(lower, -1) {
		n, _ := strconv.Atoi(m[1])
		total += n
	}
	if total == 0 && regexp.MustCompile(`^\d+$`).MatchString(lower) {
		total, _ = strconv.Atoi(lower)
	}
	if total <= 0 {
		return 0
	}
	return total
}

func TestParseDuration(t *testing.T) {
	// parity: workit reads the input the same way (checked against the port).
	// The other rows are where ytrack deliberately differs; want 0 is an error.
	for _, tt := range []struct {
		in     string
		want   int
		parity bool
	}{
		{"30m", 30, true},
		{"1h", 60, true},
		{"1h30m", 90, true},
		{"1h 30m", 90, true},
		{"90m", 90, true},
		{"90", 90, true},
		{"2H", 120, true},
		{"  1h15m ", 75, true},
		{"1 hour 30 minutes", 90, true},
		{"2 hours", 120, true},
		{"45 mins", 45, true},
		{"45min", 45, true},
		{"30m 1h", 90, true},
		{"0", 0, true},
		{"0m", 0, true},
		{"", 0, true},
		{"abc", 0, true},
		{"1d", 0, true},
		{"1w", 0, true},
		// Decimal hours: workit reads "1.5h" as "5h" (300 minutes).
		{"1.5h", 90, false},
		{"0.25h", 15, false},
		{"1.5 hours", 90, false},
		{"1.25h", 75, false},
		// workit keeps the parts it recognizes and drops the rest.
		{"1h30", 0, false},
		{"about 2h", 0, false},
		{"-30m", 0, false},
		{"1.5m", 0, false},
		{"0.01h", 0, false},
		{"2 days", 0, true},
	} {
		got, err := ParseDuration(tt.in)
		if (err != nil) != (tt.want == 0) || got != tt.want {
			t.Errorf("ParseDuration(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
		if w := workitParseDuration(tt.in); tt.parity != (w == tt.want) {
			t.Errorf("%q: workit gives %d, ytrack %d; the table says parity=%v", tt.in, w, tt.want, tt.parity)
		}
	}
}

func TestParseDurationExplainsDaysAndUnits(t *testing.T) {
	for in, want := range map[string]string{
		"1d":    "days and weeks depend on the YouTrack work schedule",
		"3s":    `unknown unit "s"`,
		"1h30":  "use hours and minutes",
		"1.5m":  "not a whole number of minutes",
		"0h":    "more than 0 minutes",
		"   ":   "empty duration",
		"1hour": "",
	} {
		_, err := ParseDuration(in)
		if want == "" {
			if err != nil {
				t.Errorf("ParseDuration(%q): %v", in, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseDuration(%q) error = %v, want it to mention %q", in, err, want)
		}
	}
}

func TestParseDurationLimits(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int
		err  string
	}{
		{"2147483647", 2147483647, ""},
		{"2147483648", 0, "too long"},
		{"99999999999999999999999", 0, "too long"},
		{"35791394h", 2147483640, ""},
		{"35791395h", 0, "too long"},
		// Exact decimals: 1.1 * 60 is 66.00000000000001 in float64.
		{"1.1h", 66, ""},
		{"0.7h", 42, ""},
		{"1.01h", 0, "not a whole number of minutes"},
		{"0.0000001h", 0, "not a whole number of minutes"},
		{"1.0000000001h", 0, "not a whole number of minutes"},
		{"1.00000001h", 0, "not a whole number of minutes"},
	} {
		got, err := ParseDuration(tt.in)
		if tt.err == "" {
			if err != nil || got != tt.want {
				t.Errorf("ParseDuration(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("ParseDuration(%q) = %d, %v; want an error mentioning %q", tt.in, got, err, tt.err)
		}
	}
}

func TestParseDayYears(t *testing.T) {
	for in, ok := range map[string]bool{
		"1970-01-01": true, "9999-12-31": true,
		"1969-12-31": false, "0001-01-01": false, "10000-01-01": false,
	} {
		if _, err := ParseDay(in); (err == nil) != ok {
			t.Errorf("ParseDay(%q) error = %v, want ok=%v", in, err, ok)
		}
	}
}

func TestFormatMinutes(t *testing.T) {
	for minutes, want := range map[int]string{45: "45m", 60: "1h", 90: "1h 30m", 600: "10h"} {
		if got := FormatMinutes(minutes); got != want {
			t.Errorf("FormatMinutes(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestWorkDate(t *testing.T) {
	santiago, tokyo := mustZone(t, "America/Santiago"), mustZone(t, "Asia/Tokyo")
	oct7 := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	oct8 := oct7.AddDate(0, 0, 1)

	t.Run("given auto just before midnight west of UTC, it is that local day although UTC is already tomorrow", func(t *testing.T) {
		now := time.Date(2026, 10, 7, 23, 59, 0, 0, santiago) // 02:59 UTC on the 8th
		got, err := WorkDate("auto", now, santiago)
		if err != nil || !got.Equal(oct7) {
			t.Errorf("got %v, %v; want %v", got, err, oct7)
		}
	})

	t.Run("given auto just after midnight east of UTC, it is that local day although UTC is still yesterday", func(t *testing.T) {
		now := time.Date(2026, 10, 8, 0, 30, 0, 0, tokyo) // 15:30 UTC on the 7th
		got, err := WorkDate("", now, tokyo)
		if err != nil || !got.Equal(oct8) {
			t.Errorf("got %v, %v; want %v", got, err, oct8)
		}
	})

	t.Run("given auto, the work timezone decides the day, not the clock's zone", func(t *testing.T) {
		now := time.Date(2026, 10, 7, 23, 59, 0, 0, santiago)
		got, _ := WorkDate("auto", now, tokyo) // 11:59 on the 8th in Tokyo
		if !got.Equal(oct8) {
			t.Errorf("got %v, want %v", got, oct8)
		}
	})

	t.Run("given a day, it is that day at midnight UTC in any timezone", func(t *testing.T) {
		for _, loc := range []*time.Location{santiago, tokyo, time.UTC} {
			now := time.Date(2026, 10, 7, 23, 59, 0, 0, loc)
			for _, in := range []string{"2026-10-07", "2026-10-7"} {
				got, err := WorkDate(in, now, loc)
				if err != nil || got.UnixMilli() != 1791331200000 {
					t.Errorf("%s in %s: got %v (%d), %v", in, loc, got, got.UnixMilli(), err)
				}
			}
		}
	})

	t.Run("given a day that does not exist, it fails", func(t *testing.T) {
		for _, in := range []string{"2026-13-01", "2026-02-30", "not-a-date", "07/10/2026", "1791331200000"} {
			if _, err := WorkDate(in, time.Now(), time.UTC); err == nil || !strings.Contains(err.Error(), "use auto or YYYY-MM-DD") {
				t.Errorf("WorkDate(%q) error = %v", in, err)
			}
		}
	})
}

func TestLocation(t *testing.T) {
	fallback := mustZone(t, "America/Santiago")
	if got, err := Location("", fallback); err != nil || got != fallback {
		t.Errorf("no setting: got %v, %v; want the process timezone", got, err)
	}
	if got, err := Location("Asia/Tokyo", fallback); err != nil || got.String() != "Asia/Tokyo" {
		t.Errorf("Asia/Tokyo: got %v, %v", got, err)
	}
	if _, err := Location("Mars/Olympus", fallback); err == nil || !strings.Contains(err.Error(), "invalid timezone \"Mars/Olympus\"") {
		t.Errorf("unknown zone: err = %v", err)
	}
}
