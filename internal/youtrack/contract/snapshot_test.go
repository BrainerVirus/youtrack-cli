package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Environment of the openapi-contract workflow: YTRACK_OPENAPI is a
// downloaded /api/openapi.json, YTRACK_OPENAPI_NAME names it in the report,
// and YTRACK_OPENAPI_SUMMARY is a file the report is appended to
// ($GITHUB_STEP_SUMMARY). YTRACK_OPENAPI_SNAPSHOT_UPDATE=1 rewrites the
// snapshot from YTRACK_OPENAPI instead of reporting.
const (
	openAPIEnv = "YTRACK_OPENAPI"
	nameEnv    = "YTRACK_OPENAPI_NAME"
	summaryEnv = "YTRACK_OPENAPI_SUMMARY"
	updateEnv  = "YTRACK_OPENAPI_SNAPSHOT_UPDATE"
)

var (
	contractFile = filepath.Join("..", "..", "..", "api", "contract", "youtrack-contract.json")
	snapshotFile = filepath.Join("..", "..", "..", "api", "contract", "openapi-snapshot.json")
)

const snapshotComment = "A digest of youtrack.jetbrains.com/api/openapi.json: every operation it lists and the attributes of the entities api/contract/youtrack-contract.json uses. The weekly openapi-contract workflow reports what changed against it without failing; refresh it with YTRACK_OPENAPI=<openapi.json> YTRACK_OPENAPI_SNAPSHOT_UPDATE=1 go test ./internal/youtrack/contract -run TestUpstreamChanges."

// contractTypes lists the entities the contract file records.
func contractTypes(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(contractFile)
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Types map[string]json.RawMessage `json:"types"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(c.Types))
	for ty := range c.Types {
		out = append(out, ty)
	}
	sort.Strings(out)
	return out
}

func TestUpstreamChanges(t *testing.T) {
	path := os.Getenv(openAPIEnv)
	if path == "" {
		t.Skip("set " + openAPIEnv + " to a YouTrack openapi.json to report its changes since the snapshot")
	}
	spec, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	now, err := Take(spec, contractTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv(updateEnv) == "1" {
		now.Comment = snapshotComment
		b, err := json.MarshalIndent(now, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(snapshotFile, append(b, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s from %s (%s)", snapshotFile, path, now.Version)
		return
	}
	b, err := os.ReadFile(snapshotFile)
	if err != nil {
		t.Fatal(err)
	}
	var recorded Snapshot
	if err := json.Unmarshal(b, &recorded); err != nil {
		t.Fatal(err)
	}
	name := os.Getenv(nameEnv)
	if name == "" {
		name = path
	}
	report := Markdown(name, recorded, now, Diff(recorded, now))
	t.Log("\n" + report)
	if out := os.Getenv(summaryEnv); out != "" {
		f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(report + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSnapshotMatchesTheContract(t *testing.T) {
	b, err := os.ReadFile(snapshotFile)
	if err != nil {
		t.Fatal(err)
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	var recorded []string
	for ty := range s.Types {
		recorded = append(recorded, ty)
	}
	sort.Strings(recorded)
	if want := contractTypes(t); !reflect.DeepEqual(recorded, want) {
		t.Errorf("the snapshot records types %v, the contract %v; refresh the snapshot", recorded, want)
	}
}

const before = `{"info":{"version":"2026.1"},
"paths":{
 "/issues":{"get":{"responses":{}},"post":{"responses":{}}},
 "/tags":{"get":{"responses":{}}}},
"components":{"schemas":{
 "Base":{"properties":{"id":{"type":"string"}}},
 "Issue":{"allOf":[{"$ref":"#/components/schemas/Base"},{"properties":{"summary":{},"votes":{}}}]},
 "Tag":{"properties":{"name":{}}}}}}`

const after = `{"info":{"version":"2026.2"},
"paths":{
 "/issues":{"get":{"responses":{},"deprecated":true},"post":{"responses":{}}},
 "/issues/{id}/holograms":{"get":{"responses":{}}}},
"components":{"schemas":{
 "Base":{"properties":{"id":{"type":"string"}}},
 "Issue":{"allOf":[{"$ref":"#/components/schemas/Base"},{"properties":{"summary":{"deprecated":true},"pinned":{}}}]},
 "Project":{"properties":{"name":{}}}}}}`

func TestTakeDigestsTheDescription(t *testing.T) {
	s, err := Take([]byte(before), []string{"Issue", "Tag", "Missing"})
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{
		Version:    "2026.1",
		Operations: []string{"GET /issues", "GET /tags", "POST /issues"},
		Types:      map[string][]string{"Issue": {"id", "summary", "votes"}, "Tag": {"name"}},
		Deprecated: []string{},
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("Take = %+v, want %+v", s, want)
	}
	if _, err := Take([]byte("{"), nil); err == nil {
		t.Error("a broken description parsed")
	}
}

func TestDiffListsUpstreamChanges(t *testing.T) {
	types := []string{"Issue", "Tag", "Project"}
	old, err := Take([]byte(before), types)
	if err != nil {
		t.Fatal(err)
	}
	cur, err := Take([]byte(after), types)
	if err != nil {
		t.Fatal(err)
	}
	got := Diff(old, cur)
	want := []Change{
		{"new", "GET /issues/{id}/holograms"},
		{"removed", "GET /tags"},
		{"new", "Project"},
		{"removed", "Tag"},
		{"new", "Issue.pinned"},
		{"removed", "Issue.votes"},
		{"deprecated", "GET /issues"},
		{"deprecated", "Issue.summary"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Diff = %v\nwant %v", got, want)
	}
	if back := Diff(cur, old); !strings.Contains(Markdown("x", cur, old, back), "no longer deprecated: `Issue.summary`") {
		t.Errorf("reverse diff misses the undeprecation: %v", back)
	}
	md := Markdown("youtrack.jetbrains.com", old, cur, got)
	for _, s := range []string{"youtrack.jetbrains.com (2026.2, snapshot 2026.1)", "8 change(s)", "- new: `GET /issues/{id}/holograms`", "- deprecated: `Issue.summary`"} {
		if !strings.Contains(md, s) {
			t.Errorf("Markdown misses %q:\n%s", s, md)
		}
	}
	if md := Markdown("x", old, old, Diff(old, old)); !strings.Contains(md, "No change since the snapshot.") {
		t.Errorf("an unchanged description: %s", md)
	}
}

// TestUpstreamChangesNeverFails runs the report against a description that
// differs from the snapshot and checks it passes and writes the summary.
func TestUpstreamChangesNeverFails(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(spec, []byte(after), 0o600); err != nil {
		t.Fatal(err)
	}
	summary := filepath.Join(dir, "summary.md")
	t.Setenv(openAPIEnv, spec)
	t.Setenv(nameEnv, "seeded")
	t.Setenv(summaryEnv, summary)
	t.Setenv(updateEnv, "")
	t.Run("report", TestUpstreamChanges)
	b, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Upstream changes: seeded (2026.2", "- removed: `IssueComment`", "- new: `GET /issues/{id}/holograms`"} {
		if !strings.Contains(string(b), s) {
			t.Errorf("summary misses %q:\n%s", s, b)
		}
	}
}
