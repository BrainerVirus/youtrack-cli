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

func TestRequestsMatchTheContract(t *testing.T) {
	c := loadContract(t)

	env := cmdtest.New(t)
	yt := cmdtest.NewFakeYouTrack(t, "")
	yt.SeedSampleIssues()
	env.Login(yt)
	allIssue := strings.Join(shared.IssueFields, ",")
	runs := [][]string{
		{"issue", "list", "-q", "#Unresolved", "--project", "NSR"},
		{"issue", "list", "--json", allIssue},
		{"issue", "list", "--json", "state,priority,assignee"},
		{"issue", "view", "NSR-40", "--comments"},
		{"issue", "view", "NSR-40", "--json", allIssue + ",comments"},
		{"issue", "comment", "NSR-40", "--body", "contract"},
	}
	for _, args := range runs {
		if code := env.Run(args...); code != 0 {
			t.Fatalf("ytrack %s: exit %d: %s", strings.Join(args, " "), code, env.Stderr)
		}
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
	if _, ok := c.endpoint("DELETE", "/api/issues/NSR-1"); ok {
		t.Error("an unlisted method matched")
	}
	if e, ok := c.endpoint("GET", "/api/users/me"); !ok || e.Path != "/users/me" {
		t.Errorf("GET /api/users/me matched %v", e)
	}
}
