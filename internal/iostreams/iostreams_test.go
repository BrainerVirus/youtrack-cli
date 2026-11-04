package iostreams_test

import (
	"os"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
)

func TestSystemColor(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want bool
	}{
		"CLICOLOR_FORCE=1":                     {env: map[string]string{"CLICOLOR_FORCE": "1"}, want: true},
		"NO_COLOR, even with CLICOLOR_FORCE":   {env: map[string]string{"CLICOLOR_FORCE": "1", "NO_COLOR": "1"}, want: false},
		"TERM=dumb, even with CLICOLOR_FORCE":  {env: map[string]string{"CLICOLOR_FORCE": "1", "TERM": "dumb"}, want: false},
		"a pipe (the test binary's streams)":   {env: map[string]string{}, want: false},
		"an empty NO_COLOR still turns it off": {env: map[string]string{"CLICOLOR_FORCE": "1", "NO_COLOR": ""}, want: false},
	}
	for name, tc := range cases {
		t.Run("given "+name+", color is "+map[bool]string{true: "on", false: "off"}[tc.want], func(t *testing.T) {
			t.Setenv("TERM", "xterm-256color")
			t.Setenv("CLICOLOR_FORCE", "")
			t.Setenv("CLICOLOR", "")
			unsetNoColor(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			ios := iostreams.System()
			if got := ios.ColorEnabled(); got != tc.want {
				t.Errorf("stdout color = %v, want %v", got, tc.want)
			}
			if got := ios.ErrColorEnabled(); got != tc.want {
				t.Errorf("stderr color = %v, want %v", got, tc.want)
			}
		})
	}
}

// unsetNoColor removes NO_COLOR for the test (t.Setenv cannot unset) and
// restores it afterwards.
func unsetNoColor(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "") // registers the restore
	_ = os.Unsetenv("NO_COLOR")
}

func TestColorSchemeOff(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	cs := ios.ColorScheme()
	for _, s := range []string{cs.Bold("a"), cs.Accent("a"), cs.State("Fixed", true), cs.Priority("Critical"), cs.Error("ytrack: `x`")} {
		if s != "a" && s != "Fixed" && s != "Critical" && s != "ytrack: `x`" {
			t.Errorf("styled without color: %q", s)
		}
	}
}

func TestColorSchemeMeaning(t *testing.T) {
	ios, _, _, _ := iostreams.Test()
	ios.SetColorEnabled(true)
	cs := ios.ColorScheme()
	for in, want := range map[string]string{
		cs.State("Fixed", false):                    "\x1b[32mFixed\x1b[0m",
		cs.State("Custom", true):                    "\x1b[32mCustom\x1b[0m",
		cs.State("In Progress", false):              "\x1b[33mIn Progress\x1b[0m",
		cs.State("Open", false):                     "\x1b[33mOpen\x1b[0m",
		cs.State("Custom", false):                   "Custom",
		cs.Priority("Critical"):                     "\x1b[31mCritical\x1b[0m",
		cs.Priority("Show-stopper"):                 "\x1b[31mShow-stopper\x1b[0m",
		cs.Priority("Normal"):                       "Normal",
		cs.Error("ytrack: run `ytrack auth login`"): "\x1b[31mytrack: run \x1b[1m`ytrack auth login`\x1b[0m\x1b[31m\x1b[0m",
	} {
		if in != want {
			t.Errorf("got %q, want %q", in, want)
		}
	}
}
