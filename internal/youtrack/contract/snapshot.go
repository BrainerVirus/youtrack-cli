// Package contract digests YouTrack's /api/openapi.json into a snapshot of
// the operations it lists and the attributes of the entities ytrack uses, and
// reports what changed upstream between two snapshots. The weekly
// openapi-contract workflow prints that report as a summary; it never fails
// the check, which is internal/youtrack/adapter's TestContractMatchesOpenAPI.
package contract

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Snapshot is the part of an OpenAPI description the contract check cares
// about, small enough to commit.
type Snapshot struct {
	Comment string `json:"$comment,omitempty"`
	// Version is the description's info.version (the YouTrack release).
	Version string `json:"version"`
	// Operations are "METHOD /path", sorted.
	Operations []string `json:"operations"`
	// Types maps each entity to its attributes, inherited ones included,
	// sorted.
	Types map[string][]string `json:"types"`
	// Deprecated lists the operations ("METHOD /path") and attributes
	// ("Entity.attribute") the description marks deprecated, sorted.
	Deprecated []string `json:"deprecated"`
}

type schema struct {
	Ref        string             `json:"$ref"`
	Properties map[string]*schema `json:"properties"`
	AllOf      []*schema          `json:"allOf"`
	Deprecated bool               `json:"deprecated"`
}

var methods = []string{"get", "put", "post", "delete", "patch", "head", "options"}

// Take digests an OpenAPI description, keeping the attributes of types only.
// A type the description does not define is left out, so Diff reports it as
// removed.
func Take(spec []byte, types []string) (Snapshot, error) {
	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]*schema `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec, &doc); err != nil {
		return Snapshot{}, fmt.Errorf("parsing the OpenAPI description: %w", err)
	}
	s := Snapshot{Version: doc.Info.Version, Operations: []string{}, Types: map[string][]string{}, Deprecated: []string{}}
	for path, item := range doc.Paths {
		for _, m := range methods {
			raw, ok := item[m]
			if !ok {
				continue
			}
			op := strings.ToUpper(m) + " " + path
			s.Operations = append(s.Operations, op)
			var o struct {
				Deprecated bool `json:"deprecated"`
			}
			if json.Unmarshal(raw, &o) == nil && o.Deprecated {
				s.Deprecated = append(s.Deprecated, op)
			}
		}
	}
	for _, ty := range types {
		if doc.Components.Schemas[ty] == nil {
			continue
		}
		props := map[string]*schema{}
		collect(doc.Components.Schemas, ty, props, map[string]bool{})
		attrs := make([]string, 0, len(props))
		for name, p := range props {
			attrs = append(attrs, name)
			if p.Deprecated {
				s.Deprecated = append(s.Deprecated, ty+"."+name)
			}
		}
		sort.Strings(attrs)
		s.Types[ty] = attrs
	}
	sort.Strings(s.Operations)
	sort.Strings(s.Deprecated)
	return s, nil
}

// collect gathers the properties of the schema called name, its own
// declarations winning over inherited ones.
func collect(schemas map[string]*schema, name string, into map[string]*schema, seen map[string]bool) {
	sc := schemas[name]
	if sc == nil || seen[name] {
		return
	}
	seen[name] = true
	parts := append([]*schema{sc}, sc.AllOf...)
	for _, part := range parts {
		for attr, p := range part.Properties {
			if _, ok := into[attr]; !ok && p != nil {
				into[attr] = p
			}
		}
	}
	for _, part := range sc.AllOf {
		if part.Ref != "" {
			collect(schemas, part.Ref[strings.LastIndexByte(part.Ref, '/')+1:], into, seen)
		}
	}
}

// Change is one difference between two snapshots.
type Change struct {
	// Kind is "new", "removed", "deprecated" or "no longer deprecated".
	Kind string
	// Item is "METHOD /path", "Entity" or "Entity.attribute".
	Item string
}

// Diff lists what changed from before to after, operations first, each group
// sorted.
func Diff(before, after Snapshot) []Change {
	var out []Change
	added, removed := setDiff(before.Operations, after.Operations)
	out = append(out, changes("new", added)...)
	out = append(out, changes("removed", removed)...)
	var oldAttrs, newAttrs []string
	types := map[string]bool{}
	for ty := range before.Types {
		types[ty] = true
	}
	for ty := range after.Types {
		types[ty] = true
	}
	var newTypes, goneTypes []string
	for ty := range types {
		o, inOld := before.Types[ty]
		n, inNew := after.Types[ty]
		switch {
		case !inOld:
			newTypes = append(newTypes, ty)
		case !inNew:
			goneTypes = append(goneTypes, ty)
		default:
			oldAttrs = append(oldAttrs, qualify(ty, o)...)
			newAttrs = append(newAttrs, qualify(ty, n)...)
		}
	}
	sort.Strings(newTypes)
	sort.Strings(goneTypes)
	out = append(out, changes("new", newTypes)...)
	out = append(out, changes("removed", goneTypes)...)
	added, removed = setDiff(oldAttrs, newAttrs)
	out = append(out, changes("new", added)...)
	out = append(out, changes("removed", removed)...)
	deprecated, undeprecated := setDiff(before.Deprecated, after.Deprecated)
	out = append(out, changes("deprecated", deprecated)...)
	out = append(out, changes("no longer deprecated", undeprecated)...)
	return out
}

func qualify(ty string, attrs []string) []string {
	out := make([]string, len(attrs))
	for i, a := range attrs {
		out[i] = ty + "." + a
	}
	return out
}

// setDiff returns the items only in b and the items only in a, sorted.
func setDiff(a, b []string) (onlyB, onlyA []string) {
	for _, x := range b {
		if !slices.Contains(a, x) {
			onlyB = append(onlyB, x)
		}
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			onlyA = append(onlyA, x)
		}
	}
	sort.Strings(onlyB)
	sort.Strings(onlyA)
	return onlyB, onlyA
}

func changes(kind string, items []string) []Change {
	out := make([]Change, len(items))
	for i, it := range items {
		out[i] = Change{Kind: kind, Item: it}
	}
	return out
}

// Markdown renders the changes of the description called name against the
// snapshot as a short report.
func Markdown(name string, before, after Snapshot, cs []Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Upstream changes: %s (%s, snapshot %s)\n\n", name, orUnknown(after.Version), orUnknown(before.Version))
	if len(cs) == 0 {
		b.WriteString("No change since the snapshot.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d change(s) since the snapshot. Informational only: the contract check above decides pass or fail.\n\n", len(cs))
	for _, c := range cs {
		fmt.Fprintf(&b, "- %s: `%s`\n", c.Kind, c.Item)
	}
	return b.String()
}

func orUnknown(v string) string {
	if v == "" {
		return "version unknown"
	}
	return v
}
