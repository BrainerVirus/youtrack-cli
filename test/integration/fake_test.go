package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
)

// These tests run the suite against a fake YouTrack on loopback, so they
// prove its command paths and cleanup without YTRACK_IT and without a
// real server.

var (
	binOnce sync.Once
	binPath string
	binErr  error
	binDir  string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// sharedYtrack builds ytrack once for all fake-server tests.
func sharedYtrack(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		binDir, binErr = os.MkdirTemp("", "ytrack-it-bin-")
		if binErr != nil {
			return
		}
		binPath = filepath.Join(binDir, "ytrack")
		out, err := exec.Command("go", "build", "-o", binPath, "github.com/BrainerVirus/youtrack-cli/cmd/ytrack").CombinedOutput()
		if err != nil {
			binErr = fmt.Errorf("building ytrack: %w\n%s", err, out)
		}
	})
	if binErr != nil {
		t.Fatal(binErr)
	}
	return binPath
}

func mustParse(t *testing.T, url string) hosts.Host {
	t.Helper()
	h, err := hosts.Parse(url)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// fakeInstance fronts cmdtest's FakeYouTrack with what the suite needs on
// top of it: issue deletion (cascading to comments and work items, as in
// YouTrack) and injected failures. Requests are served one at a time.
type fakeInstance struct {
	yt     *cmdtest.FakeYouTrack
	server *httptest.Server
	mu     sync.Mutex
	// failBefore answers 500 without touching the fake.
	failBefore func(*http.Request) bool
	// failAfter lets the fake apply the request, then answers 500, as when
	// a response is lost after the server acted.
	failAfter func(*http.Request) bool
	deleted   []string
}

func newFakeInstance(t *testing.T) *fakeInstance {
	t.Helper()
	yt := cmdtest.NewFakeYouTrack(t, "")
	yt.SeedSampleProjects()
	yt.SeedSampleWorkItems() // NSR time tracking settings
	yt.Issues = nil
	yt.WorkItems = map[string][]map[string]any{}
	f := &fakeInstance{yt: yt}
	inner := yt.Server.Config.Handler
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/issues/") && strings.Count(r.URL.Path, "/") == 3 {
			f.deleteIssue(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/commands") && !f.localizeCommand(w, r) {
			return
		}
		if f.failBefore != nil && f.failBefore(r) {
			http.Error(w, `{"error":"server_error","error_description":"injected failure"}`, http.StatusInternalServerError)
			return
		}
		if f.failAfter != nil && f.failAfter(r) {
			inner.ServeHTTP(httptest.NewRecorder(), r)
			http.Error(w, `{"error":"server_error","error_description":"injected failure after apply"}`, http.StatusInternalServerError)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeInstance) deleteIssue(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.yt.Token {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/issues/")
	i := slices.IndexFunc(f.yt.Issues, func(is map[string]any) bool { return is["idReadable"] == id || is["id"] == id })
	if i < 0 {
		http.Error(w, `{"error":"Not Found"}`, http.StatusNotFound)
		return
	}
	readable := fmt.Sprint(f.yt.Issues[i]["idReadable"])
	f.yt.Issues = slices.Delete(f.yt.Issues, i, i+1)
	delete(f.yt.Comments, readable)
	delete(f.yt.WorkItems, readable)
	f.deleted = append(f.deleted, readable)
	w.WriteHeader(http.StatusOK)
}

// localizeCommand makes the fake behave like a non-English instance: the
// "for" keyword is unknown, and "Assignee <login>" (a field name, which works
// on every locale) is applied as cmdtest's fake understands it. It answers
// and returns false for a rejected command.
func (f *fakeInstance) localizeCommand(w http.ResponseWriter, r *http.Request) bool {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err == nil {
		query, _ := body["query"].(string)
		if word, _, _ := strings.Cut(query, " "); strings.EqualFold(word, "for") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"bad_request","error_description":"Comando desconocido: for"}`)
			return false
		}
		if login, ok := strings.CutPrefix(query, "Assignee "); ok {
			body["query"] = "for " + login
			raw, _ = json.Marshal(body)
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	return true
}

// leftovers lists what the run left on the fake.
func (f *fakeInstance) leftovers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, is := range f.yt.Issues {
		out = append(out, "issue "+fmt.Sprint(is["idReadable"]))
	}
	for id, items := range f.yt.WorkItems {
		for _, wi := range items {
			out = append(out, fmt.Sprintf("work item %v on %s", wi["id"], id))
		}
	}
	return out
}

// runAgainst runs the suite against f, as YTRACK_IT=1 would, with an
// isolated config dir and the fake's token in YTRACK_IT_TOKEN.
func (f *fakeInstance) runAgainst(t *testing.T) error {
	t.Helper()
	cfg, err := loadConfig(env(map[string]string{
		envProject: "NSR",
		envHost:    f.server.URL,
		envToken:   f.yt.Token,
		envAllow:   f.server.URL,
	}), noDefaultHost)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Bin = sharedYtrack(t)
	base := append(os.Environ(), "YTRACK_CONFIG_DIR="+t.TempDir())
	return newSuite(cfg, base, t.Logf).run()
}

func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs the ytrack binary")
	}
}

func TestSuiteAgainstFakeServer(t *testing.T) {
	skipShort(t)
	f := newFakeInstance(t)

	if err := f.runAgainst(t); err != nil {
		t.Fatal(err)
	}

	if left := f.leftovers(); len(left) != 0 {
		t.Errorf("the run left %v", left)
	}
	if len(f.deleted) != 1 {
		t.Errorf("deleted issues %v, want the one the run created", f.deleted)
	}
	// Every command path reached the server.
	seen := map[string]bool{}
	for _, r := range f.yt.Requests() {
		seen[r.Method+" "+routeOf(r.Path)] = true
	}
	for _, want := range []string{
		"GET /api/users/me",
		"POST /api/issues",
		"GET /api/issues/{id}",
		"GET /api/issues",
		"POST /api/issues/{id}",
		"POST /api/issues/{id}/comments",
		"POST /api/commands/assist",
		"POST /api/commands",
		"POST /api/issues/{id}/timeTracking/workItems",
		"GET /api/issues/{id}/timeTracking/workItems",
		"POST /api/issues/{id}/timeTracking/workItems/{item}",
		"DELETE /api/issues/{id}/timeTracking/workItems/{item}",
	} {
		if !seen[want] {
			t.Errorf("no %s request; saw %v", want, keys(seen))
		}
	}
}

func TestSuiteCleansUpWhenAStepFails(t *testing.T) {
	skipShort(t)
	f := newFakeInstance(t)
	f.failBefore = func(r *http.Request) bool {
		return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments")
	}

	err := f.runAgainst(t)

	if err == nil || !strings.Contains(err.Error(), "issue comment") {
		t.Fatalf("error %v, want the failing issue comment step", err)
	}
	if left := f.leftovers(); len(left) != 0 {
		t.Errorf("the failed run left %v", left)
	}
}

func TestSuiteCleansUpAWorkItemStepFailure(t *testing.T) {
	skipShort(t)
	f := newFakeInstance(t)
	f.failBefore = func(r *http.Request) bool {
		return r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/workItems/")
	}

	err := f.runAgainst(t)

	if err == nil || !strings.Contains(err.Error(), "work-item delete") {
		t.Fatalf("error %v, want the failing work-item delete step", err)
	}
	if left := f.leftovers(); len(left) != 0 {
		t.Errorf("the failed run left %v", left)
	}
}

func TestSuiteSweepsAnIssueWhoseCreateResponseWasLost(t *testing.T) {
	skipShort(t)
	f := newFakeInstance(t)
	f.failAfter = func(r *http.Request) bool {
		return r.Method == http.MethodPost && r.URL.Path == "/api/issues"
	}

	err := f.runAgainst(t)

	if err == nil || !strings.Contains(err.Error(), "issue create") {
		t.Fatalf("error %v, want the failing issue create step", err)
	}
	if len(f.deleted) != 1 {
		t.Errorf("deleted %v, want the issue the lost create made", f.deleted)
	}
	if left := f.leftovers(); len(left) != 0 {
		t.Errorf("the failed run left %v", left)
	}
}

func TestSuiteReportsACleanupFailure(t *testing.T) {
	skipShort(t)
	f := newFakeInstance(t)
	f.failBefore = func(r *http.Request) bool {
		return r.Method == http.MethodGet && r.URL.Path == "/api/issues" && strings.Contains(r.URL.RawQuery, "created")
	}

	err := f.runAgainst(t)

	if err == nil || !strings.Contains(err.Error(), "delete leftovers by hand") {
		t.Fatalf("error %v, want the cleanup failure with the run's prefix", err)
	}
}

// routeOf maps a request path to its fake route pattern.
func routeOf(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 3 && parts[2] == "issues" {
		parts[3] = "{id}"
	}
	if len(parts) > 6 && parts[5] == "workItems" {
		parts[6] = "{item}"
	}
	return strings.Join(parts, "/")
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
