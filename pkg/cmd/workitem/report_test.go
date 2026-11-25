package workitem_test

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestWorkItemReport(t *testing.T) {
	t.Run("given no flags, it sums your work items per issue and per day", func(t *testing.T) {
		env, yt := loggedIn(t)
		mustRun(t, env, "work-item", "report", "--json", "author,since,minutes,duration,issues,days")
		reqs := requests(yt, http.MethodGet, "/api/workItems")
		q, _ := url.ParseQuery(reqs[0].RawQuery)
		if q.Get("author") != "jdoe" || q.Has("startDate") || q.Has("query") {
			t.Errorf("query = %v", q)
		}
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		want := map[string]any{
			"author": "jdoe", "since": nil, "minutes": float64(570), "duration": "9h 30m",
			"issues": []any{map[string]any{"issue": "NSR-40", "url": yt.URL() + "/issue/NSR-40", "minutes": float64(570), "duration": "9h 30m"}},
			"days": []any{
				map[string]any{"date": "2026-09-30", "minutes": float64(90), "duration": "1h 30m"},
				map[string]any{"date": "2026-10-05", "minutes": float64(480), "duration": "8h"},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got  %v\nwant %v", got, want)
		}
	})

	t.Run("given --author, --since, --until and --project, it sends them as filters", func(t *testing.T) {
		env, yt := loggedIn(t)
		mustRun(t, env, "work-item", "report", "--author", "jroe", "--since", "2026-10-01", "--until", "2026-10-31", "--project", "NSR", "--json", "items,since,until,project")
		q, _ := url.ParseQuery(requests(yt, http.MethodGet, "/api/workItems")[0].RawQuery)
		want := url.Values{"author": {"jroe"}, "startDate": {"2026-10-01"}, "endDate": {"2026-10-31"}, "query": {"project: {NSR}"}}
		for k, v := range want {
			if !reflect.DeepEqual(q[k], v) {
				t.Errorf("%s = %v, want %v", k, q[k], v)
			}
		}
		got := decodeJSON[map[string]any](t, env.Stdout.Bytes())
		items, _ := got["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != "115-2" || got["until"] != "2026-10-31" || got["project"] != "NSR" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("given no --json, it prints the issue and day tables and the total", func(t *testing.T) {
		env, _ := loggedIn(t)
		mustRun(t, env, "work-item", "report")
		out := env.Stdout.String()
		for _, want := range []string{"NSR-40\t9h 30m\n", "2026-09-30\t1h 30m\n", "2026-10-05\t8h\n", "total\t9h 30m\n"} {
			if !strings.Contains(out, want) {
				t.Errorf("stdout lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("given a range with no work, it says so on stderr", func(t *testing.T) {
		env, _ := loggedIn(t)
		mustRun(t, env, "work-item", "report", "--since", "2030-01-01")
		if env.Stdout.Len() != 0 || !strings.Contains(env.Stderr.String(), "no work items by jdoe match") {
			t.Errorf("stdout %q, stderr %q", env.Stdout, env.Stderr)
		}
	})

	for _, c := range [][]string{
		{"--since", "yesterday"},
		{"--since", "2026-10-05", "--until", "2026-10-01"},
		{"--author", " "},
		{"--project", "{NSR}"},
	} {
		t.Run("given "+strings.Join(c, " ")+", it is a usage error and sends nothing", func(t *testing.T) {
			env, yt := loggedIn(t)
			before := len(yt.Requests())
			if code := env.Run(append([]string{"work-item", "report"}, c...)...); code != 1 {
				t.Errorf("exit %d: %s", code, env.Stderr)
			}
			if n := len(yt.Requests()) - before; n != 0 {
				t.Errorf("it sent %d requests", n)
			}
		})
	}
}
