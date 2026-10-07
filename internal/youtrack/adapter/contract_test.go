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

// The snapshot of YouTrack's REST API description. Refresh it from any
// instance's /api/openapi.json (it was taken from youtrack.jetbrains.com).
var snapshot = filepath.Join("..", "..", "..", "api", "openapi", "youtrack.json")

type schema struct {
	Ref           string             `json:"$ref"`
	Items         *schema            `json:"items"`
	Properties    map[string]*schema `json:"properties"`
	AllOf         []*schema          `json:"allOf"`
	Discriminator *struct {
		Mapping map[string]string `json:"mapping"`
	} `json:"discriminator"`
}

type operation struct {
	Parameters []struct {
		Name string `json:"name"`
		In   string `json:"in"`
	} `json:"parameters"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema *schema `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
}

type spec struct {
	paths    map[string]map[string]operation
	schemas  map[string]*schema
	children map[string][]string
}

func loadSpec(t *testing.T) *spec {
	t.Helper()
	b, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatalf("reading the OpenAPI snapshot: %v", err)
	}
	var doc struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]*schema `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	s := &spec{paths: map[string]map[string]operation{}, schemas: doc.Components.Schemas, children: map[string][]string{}}
	for p, item := range doc.Paths {
		s.paths[p] = map[string]operation{}
		for method, raw := range item {
			var op operation
			if json.Unmarshal(raw, &op) == nil && op.Responses != nil {
				s.paths[p][strings.ToUpper(method)] = op
			}
		}
	}
	for name, sc := range s.schemas {
		for _, parent := range sc.AllOf {
			if parent.Ref != "" {
				p := refName(parent.Ref)
				s.children[p] = append(s.children[p], name)
			}
		}
		if sc.Discriminator != nil {
			for _, ref := range sc.Discriminator.Mapping {
				if c := refName(ref); c != name {
					s.children[name] = append(s.children[name], c)
				}
			}
		}
	}
	return s
}

func refName(ref string) string { return ref[strings.LastIndexByte(ref, '/')+1:] }

// family is name and every schema derived from it.
func (s *spec) family(name string) []string {
	out := []string{name}
	for i := 0; i < len(out); i++ {
		for _, c := range s.children[out[i]] {
			if !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	return out
}

// property finds attr on the schema called name: its own declaration first
// (a subtype narrows an inherited property), then inherited ones.
func (s *spec) property(name, attr string) *schema {
	sc := s.schemas[name]
	if sc == nil {
		return nil
	}
	if p := sc.Properties[attr]; p != nil {
		return p
	}
	for _, part := range sc.AllOf {
		if p := part.Properties[attr]; part.Ref == "" && p != nil {
			return p
		}
	}
	for _, part := range sc.AllOf {
		if part.Ref != "" {
			if p := s.property(refName(part.Ref), attr); p != nil {
				return p
			}
		}
	}
	return nil
}

func targets(sc *schema) []string {
	switch {
	case sc == nil:
		return nil
	case sc.Ref != "":
		return []string{refName(sc.Ref)}
	case sc.Items != nil:
		return targets(sc.Items)
	}
	return nil
}

// checkProjection reports attributes in p that none of the types (or their
// subtypes) has, and nested projections on attributes that are not entities.
func (s *spec) checkProjection(types []string, p cmdtest.Projection, path string) []string {
	var problems []string
	var candidates []string
	for _, ty := range types {
		for _, f := range s.family(ty) {
			if !slices.Contains(candidates, f) {
				candidates = append(candidates, f)
			}
		}
	}
	names := make([]string, 0, len(p))
	for n := range p {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, attr := range names {
		var next []string
		found := false
		for _, c := range candidates {
			if prop := s.property(c, attr); prop != nil {
				found = true
				for _, tg := range targets(prop) {
					if !slices.Contains(next, tg) {
						next = append(next, tg)
					}
				}
			}
		}
		switch {
		case !found:
			problems = append(problems, path+attr+" is not an attribute of "+strings.Join(types, "|"))
		case p[attr] != nil && len(next) == 0:
			problems = append(problems, path+attr+" is not an entity but has a nested projection")
		case p[attr] != nil:
			problems = append(problems, s.checkProjection(next, p[attr], path+attr+".")...)
		}
	}
	return problems
}

// match finds the path template for apiPath, preferring literal segments
// (/users/me over /users/{id}).
func (s *spec) match(apiPath string) (string, bool) {
	segs := strings.Split(strings.TrimPrefix(apiPath, "/api"), "/")
	best, bestVars := "", len(segs)+1
	for tmpl := range s.paths {
		ts := strings.Split(tmpl, "/")
		if len(ts) != len(segs) {
			continue
		}
		ok := true
		for i := range ts {
			if ts[i] != segs[i] && !strings.HasPrefix(ts[i], "{") {
				ok = false
				break
			}
		}
		if vars := strings.Count(tmpl, "{"); ok && vars < bestVars {
			best, bestVars = tmpl, vars
		}
	}
	return best, best != ""
}

func TestRequestsMatchTheOpenAPISnapshot(t *testing.T) {
	s := loadSpec(t)

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
		tmpl, ok := s.match(r.Path)
		if !ok {
			t.Errorf("%s %s: no such path in the snapshot", r.Method, r.Path)
			continue
		}
		op, ok := s.paths[tmpl][r.Method]
		if !ok {
			t.Errorf("%s %s: the snapshot has no %s on %s", r.Method, r.Path, r.Method, tmpl)
			continue
		}
		covered[r.Method+" "+tmpl] = true
		q, err := url.ParseQuery(r.RawQuery)
		if err != nil {
			t.Errorf("%s %s: bad query: %v", r.Method, r.Path, err)
			continue
		}
		for name := range q {
			if !slices.ContainsFunc(op.Parameters, func(p struct {
				Name string `json:"name"`
				In   string `json:"in"`
			},
			) bool {
				return p.Name == name && p.In == "query"
			}) {
				t.Errorf("%s %s: query parameter %q is not in the snapshot", r.Method, tmpl, name)
			}
		}
		if fields := q.Get("fields"); fields != "" {
			proj, err := cmdtest.ParseProjection(fields)
			if err != nil {
				t.Errorf("%s %s: %v", r.Method, tmpl, err)
				continue
			}
			resp := op.Responses["200"].Content["application/json"].Schema
			for _, p := range s.checkProjection(targets(resp), proj, "") {
				t.Errorf("%s %s fields=%s: %s", r.Method, tmpl, fields, p)
			}
		}
	}
	for _, want := range []string{"GET /users/me", "GET /issues", "GET /issues/{id}", "GET /issues/{id}/comments", "POST /issues/{id}/comments"} {
		if !covered[want] {
			t.Errorf("no request exercised %s", want)
		}
	}
}

func TestContractCheckerCatchesMistakes(t *testing.T) {
	s := loadSpec(t)
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
		got := strings.Join(s.checkProjection([]string{"Issue"}, proj, ""), "; ")
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("fields=%s: problems %q, want %q", fields, got, want)
		}
	}
}
