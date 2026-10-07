package version

import "testing"

func TestFormat(t *testing.T) {
	tests := []struct{ name, version, commit, date, want string }{
		{"a release shows version, date and commit", "1.2.0", "abc123", "2026-10-07", "ytrack version 1.2.0 (2026-10-07)\ncommit abc123\n"},
		{"a dev build shows only the version", "dev", "", "", "ytrack version dev\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Format(tt.version, tt.commit, tt.date); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
