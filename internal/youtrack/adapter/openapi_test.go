package adapter_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// openAPIEnv names a YouTrack /api/openapi.json to check the contract
// against. The openapi-contract workflow downloads the live one and sets it;
// without it the test is skipped. The schema is never committed.
const openAPIEnv = "YTRACK_OPENAPI"

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

func loadSpec(t *testing.T, path string) *spec {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the OpenAPI description: %v", err)
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

// hasTarget reports whether some type in family(owner) declares attr as
// holding want or one of its subtypes.
func (s *spec) hasTarget(owner, attr, want string) bool {
	for _, ty := range s.family(owner) {
		for _, tg := range targets(s.property(ty, attr)) {
			if slices.Contains(s.family(want), tg) {
				return true
			}
		}
	}
	return false
}

func (s *spec) hasAttr(owner, attr string) bool {
	for _, ty := range s.family(owner) {
		if s.property(ty, attr) != nil {
			return true
		}
	}
	return false
}

func TestContractMatchesOpenAPI(t *testing.T) {
	path := os.Getenv(openAPIEnv)
	if path == "" {
		t.Skip("set " + openAPIEnv + " to a YouTrack openapi.json to check the contract against it")
	}
	s := loadSpec(t, path)
	c := loadContract(t)

	for _, e := range c.Endpoints {
		name := e.Method + " " + e.Path
		item, ok := s.paths[e.Path]
		if !ok {
			t.Errorf("%s: the path is gone", name)
			continue
		}
		op, ok := item[e.Method]
		if !ok {
			t.Errorf("%s: the method is gone", name)
			continue
		}
		for _, q := range e.Query {
			if !slices.ContainsFunc(op.Parameters, func(p struct {
				Name string `json:"name"`
				In   string `json:"in"`
			},
			) bool {
				return p.Name == q && p.In == "query"
			}) {
				t.Errorf("%s: query parameter %q is gone", name, q)
			}
		}
		if e.Response == "" {
			continue // ytrack reads no body
		}
		resp := op.Responses["200"].Content["application/json"].Schema
		if !slices.ContainsFunc(targets(resp), func(tg string) bool { return slices.Contains(s.family(e.Response), tg) }) {
			t.Errorf("%s: it no longer returns %s (returns %v)", name, e.Response, targets(resp))
		}
	}
	for ty, attrs := range c.Types {
		if s.schemas[ty] == nil {
			t.Errorf("type %s is gone", ty)
			continue
		}
		for attr, holds := range attrs {
			if !s.hasAttr(ty, attr) {
				t.Errorf("%s.%s is gone", ty, attr)
				continue
			}
			for _, want := range holds {
				if !s.hasTarget(ty, attr, want) {
					t.Errorf("%s.%s no longer holds %s", ty, attr, want)
				}
			}
		}
	}
}

func TestOpenAPICheckCatchesDrift(t *testing.T) {
	// A tiny schema in which Issue lost "summary" and project changed type.
	dir := t.TempDir()
	doc := `{"paths":{"/issues":{"get":{"parameters":[{"name":"fields","in":"query"}],"responses":{"200":{"content":{"application/json":{"schema":{"type":"array","items":{"$ref":"#/components/schemas/Issue"}}}}}}}}},
"components":{"schemas":{
 "Issue":{"properties":{"idReadable":{"type":"string"},"project":{"$ref":"#/components/schemas/Folder"}}},
 "Folder":{"properties":{"name":{"type":"string"}}},
 "Project":{"properties":{"name":{"type":"string"}}}}}}`
	path := dir + "/openapi.json"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	s := loadSpec(t, path)
	if !s.hasAttr("Issue", "idReadable") || s.hasAttr("Issue", "summary") {
		t.Error("hasAttr is wrong")
	}
	if s.hasTarget("Issue", "project", "Project") || !s.hasTarget("Issue", "project", "Folder") {
		t.Error("hasTarget is wrong")
	}
}
