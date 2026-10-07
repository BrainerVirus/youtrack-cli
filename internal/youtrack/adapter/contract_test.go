package adapter_test

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/cmdtest"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	workitem "github.com/BrainerVirus/youtrack-cli/pkg/cmd/workitem/shared"
)

// contractFile is the hand-written list of the REST API surface ytrack uses.
var contractFile = filepath.Join("..", "..", "..", "api", "contract", "youtrack-contract.json")

type endpoint struct {
	Method   string   `json:"method"`
	Path     string   `json:"path"`
	Query    []string `json:"query"`
	Response string   `json:"response"`
}

type contract struct {
	Endpoints []endpoint `json:"endpoints"`
	// Types maps an entity to its attributes and the entities each may hold.
	Types map[string]map[string][]string `json:"types"`
}

func loadContract(t *testing.T) *contract {
	t.Helper()
	b, err := os.ReadFile(contractFile)
	if err != nil {
		t.Fatalf("reading the contract: %v", err)
	}
	var c contract
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("parsing the contract: %v", err)
	}
	return &c
}

// endpoint finds the contract entry for a request path under /api,
// preferring literal segments (/users/me over /users/{id}).
func (c *contract) endpoint(method, apiPath string) (endpoint, bool) {
	segs := strings.Split(strings.TrimPrefix(apiPath, "/api"), "/")
	var best endpoint
	bestVars := len(segs) + 1
	for _, e := range c.Endpoints {
		ts := strings.Split(e.Path, "/")
		if e.Method != method || len(ts) != len(segs) {
			continue
		}
		ok := true
		for i := range ts {
			if ts[i] != segs[i] && !strings.HasPrefix(ts[i], "{") {
				ok = false
				break
			}
		}
		if vars := strings.Count(e.Path, "{"); ok && vars < bestVars {
			best, bestVars = e, vars
		}
	}
	return best, best.Path != ""
}

// checkProjection reports attributes in p that none of the types has, and
// nested projections on scalar attributes.
func (c *contract) checkProjection(types []string, p cmdtest.Projection, path string) []string {
	var problems []string
	names := make([]string, 0, len(p))
	for n := range p {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, attr := range names {
		var next []string
		found := false
		for _, ty := range types {
			holds, ok := c.Types[ty][attr]
			if !ok {
				continue
			}
			found = true
			for _, h := range holds {
				if !slices.Contains(next, h) {
					next = append(next, h)
				}
			}
		}
		switch {
		case !found:
			problems = append(problems, path+attr+" is not an attribute of "+strings.Join(types, "|"))
		case p[attr] != nil && len(next) == 0:
			problems = append(problems, path+attr+" is not an entity but has a nested projection")
		case p[attr] != nil:
			problems = append(problems, c.checkProjection(next, p[attr], path+attr+".")...)
		}
	}
	return problems
}

// checkBody reports attributes of a JSON request body that the endpoint's
// entity does not have: ytrack writes only attributes it could read back.
func (c *contract) checkBody(e endpoint, body string) []string {
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return []string{"not JSON: " + err.Error()}
	}
	m, ok := v.(map[string]any)
	if !ok || e.Response == "" {
		return []string{"unexpected body"}
	}
	return c.checkProjection([]string{e.Response}, bodyProjection(m), "")
}

// bodyProjection turns a JSON object into the projection of its attributes,
// merging the objects in arrays. $type is left out: every entity has it.
func bodyProjection(m map[string]any) cmdtest.Projection {
	p := cmdtest.Projection{}
	for k, v := range m {
		if k == "$type" {
			continue
		}
		p[k] = valueProjection(v)
	}
	return p
}

func valueProjection(v any) cmdtest.Projection {
	switch t := v.(type) {
	case map[string]any:
		return bodyProjection(t)
	case []any:
		var merged cmdtest.Projection
		for _, e := range t {
			if sub := valueProjection(e); sub != nil {
				merged = mergeProjection(merged, sub)
			}
		}
		return merged
	}
	return nil
}

// mergeProjection merges b into a, keeping nested attributes of both.
func mergeProjection(a, b cmdtest.Projection) cmdtest.Projection {
	if a == nil {
		a = cmdtest.Projection{}
	}
	for k, v := range b {
		if v == nil {
			if _, ok := a[k]; !ok {
				a[k] = nil
			}
			continue
		}
		a[k] = mergeProjection(a[k], v)
	}
	return a
}

func TestRequestsMatchTheContract(t *testing.T) {
	c := loadContract(t)

	env := cmdtest.New(t)
	yt := cmdtest.NewFakeYouTrack(t, "")
	yt.SeedSampleIssues()
	yt.SeedSampleWorkItems()
	yt.SeedSampleProjects()
	env.Login(yt)
	allIssue := strings.Join(shared.IssueFields, ",")
	allWorkItem := strings.Join(workitem.Fields, ",")
	runs := [][]string{
		{"issue", "list", "-q", "#Unresolved", "--project", "NSR"},
		{"issue", "list", "--json", allIssue},
		{"issue", "list", "--json", "state,priority,assignee"},
		{"issue", "view", "NSR-40", "--comments"},
		{"issue", "view", "NSR-40", "--json", allIssue + ",comments"},
		{"issue", "comment", "NSR-40", "--body", "contract"},
		{"work-item", "list", "NSR-40", "--author", "me", "--json", allWorkItem},
		{"work-item", "add", "NSR-40", "--duration", "1h30m", "--type", "Testing", "--text", "contract", "--date", "2026-10-06"},
		{"work-item", "edit", "NSR-40", "115-1", "--duration", "2h", "--type", "Documentation", "--text", "", "--date", "2026-10-05"},
		{"work-item", "delete", "NSR-40", "115-2", "--yes"},
		{
			"issue", "create", "-p", "NSR", "-s", "contract", "--description", "body", "--assignee", "me", "--tag", "backend",
			"--field", "Priority=Critical", "--field", "Subsystem=Auth,Web", "--field", "Due Date=2026-10-20",
			"--field", "Story points=3", "--field", "Deployed=2026-10-07T10:00:00Z", "--field", "Root cause=cookie",
			"--field", "Estimation=2d", "--json", allIssue,
		},
		{"issue", "edit", "NSR-40", "-s", "edited", "-d", "", "--field", "Type=Feature", "--add-tag", "regression", "--remove-tag", "sso", "--json", allIssue},
		{"issue", "command", "NSR-40", "State Fixed", "--comment", "done", "--silent"},
		{"issue", "command", "NSR-40", "Priority Critical", "--dry-run"},
	}
	for _, args := range runs {
		if code := env.Run(args...); code != 0 {
			t.Fatalf("ytrack %s: exit %d: %s", strings.Join(args, " "), code, env.Stderr)
		}
	}
	// The interactive delete reads the work item before asking.
	env.Interactive()
	env.Stdin.WriteString("y\n")
	if code := env.Run("work-item", "delete", "NSR-40", "115-3"); code != 0 {
		t.Fatalf("interactive delete: exit %d: %s", code, env.Stderr)
	}

	covered := map[string]bool{}
	for _, r := range yt.Requests() {
		e, ok := c.endpoint(r.Method, r.Path)
		if !ok {
			t.Errorf("%s %s: not in the contract", r.Method, r.Path)
			continue
		}
		covered[e.Method+" "+e.Path] = true
		q, err := url.ParseQuery(r.RawQuery)
		if err != nil {
			t.Errorf("%s %s: bad query: %v", r.Method, r.Path, err)
			continue
		}
		for name := range q {
			if !slices.Contains(e.Query, name) {
				t.Errorf("%s %s: query parameter %q is not in the contract", e.Method, e.Path, name)
			}
		}
		if r.Body != "" {
			for _, p := range c.checkBody(e, r.Body) {
				t.Errorf("%s %s body %s: %s", e.Method, e.Path, r.Body, p)
			}
		}
		if fields := q.Get("fields"); fields != "" {
			proj, err := cmdtest.ParseProjection(fields)
			if err != nil {
				t.Errorf("%s %s: %v", e.Method, e.Path, err)
				continue
			}
			for _, p := range c.checkProjection([]string{e.Response}, proj, "") {
				t.Errorf("%s %s fields=%s: %s", e.Method, e.Path, fields, p)
			}
		}
	}
	for _, e := range c.Endpoints {
		if !covered[e.Method+" "+e.Path] {
			t.Errorf("no request exercised %s %s; drop it from the contract or test it", e.Method, e.Path)
		}
	}
}

func TestContractCheckerCatchesMistakes(t *testing.T) {
	c := loadContract(t)
	for fields, want := range map[string]string{
		"idReadable,summery":                   "summery is not an attribute of Issue",
		"customFields(name,value(nickname))":   "customFields.value.nickname is not an attribute",
		"summary(name)":                        "summary is not an entity",
		"customFields(name,value(name,login))": "",
	} {
		proj, err := cmdtest.ParseProjection(fields)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(c.checkProjection([]string{"Issue"}, proj, ""), "; ")
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("fields=%s: problems %q, want %q", fields, got, want)
		}
	}
	if got := c.checkBody(endpoint{Response: "IssueWorkItem"}, `{"duration":{"mins":5},"text":"x"}`); len(got) != 1 || !strings.Contains(got[0], "duration.mins is not an attribute") {
		t.Errorf("a body with an unknown attribute: problems %q", got)
	}
	if got := c.checkBody(endpoint{Response: "Issue"}, `{"customFields":[{"name":"A","$type":"X","value":{"name":"x"}},{"name":"B","value":[{"nick":"y"}]}]}`); len(got) != 1 || !strings.Contains(got[0], "customFields.value.nick is not an attribute") {
		t.Errorf("a body with an unknown attribute in an array: problems %q", got)
	}
	if _, ok := c.endpoint("DELETE", "/api/issues/NSR-1"); ok {
		t.Error("an unlisted method matched")
	}
	if e, ok := c.endpoint("GET", "/api/users/me"); !ok || e.Path != "/users/me" {
		t.Errorf("GET /api/users/me matched %v", e)
	}
}
