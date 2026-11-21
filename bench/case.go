package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Case is one bench task: a prompt, the world it runs in and how it is graded.
type Case struct {
	ID       string            `json:"id"`
	About    string            `json:"about"`
	Prompt   string            `json:"prompt"`
	MaxTurns int               `json:"maxTurns"`
	Files    map[string]string `json:"files,omitempty"`
	Fixtures Fixtures          `json:"fixtures"`
	Require  []Rule            `json:"require"`
	Forbid   []Rule            `json:"forbid,omitempty"`
	// Answer, when set, must match the model's final message (case-insensitive).
	Answer string `json:"answer,omitempty"`

	hash string
}

// Rule matches lines of one source: "calls" (the fake ytrack's argv, one line
// per call, the default) or "commands" (each Bash command the model ran).
// A line matches when every Match pattern and no Not pattern matches it.
// A required call must also have exited 0.
type Rule struct {
	Name  string   `json:"name"`
	On    string   `json:"on,omitempty"`
	Match []string `json:"match"`
	Not   []string `json:"not,omitempty"`
	// Before (require only): the first matching line comes before the first
	// other line matching every Before pattern.
	Before []string `json:"before,omitempty"`
	// Times (forbid only): the rule breaks at this many matching lines
	// (default 1); 2 catches a write that was repeated.
	Times int `json:"times,omitempty"`
}

// Fixtures are the canned YouTrack the fake ytrack answers from.
type Fixtures struct {
	Issues map[string]map[string]any `json:"issues,omitempty"`
	List   []map[string]any          `json:"list,omitempty"`
	Rules  []Reply                   `json:"rules,omitempty"`
}

// Reply answers a call whose argv line matches Match, before the built-in replies.
type Reply struct {
	Match  string `json:"match"`
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	Exit   int    `json:"exit,omitempty"`
}

func loadCases(dir string, only []string) ([]*Case, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var cases []*Case
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		c, err := parseCase(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if len(only) == 0 || slices.Contains(only, c.ID) {
			cases = append(cases, c)
		}
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("no cases in %s", dir)
	}
	return cases, nil
}

func parseCase(b []byte) (*Case, error) {
	var c Case
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if c.ID == "" || c.Prompt == "" || len(c.Require) == 0 {
		return nil, fmt.Errorf("a case needs id, prompt and at least one require rule")
	}
	if c.MaxTurns == 0 {
		c.MaxTurns = 10
	}
	rules := slices.Concat(c.Require, c.Forbid)
	for _, r := range rules {
		if r.Name == "" || len(r.Match) == 0 || (r.On != "" && r.On != "calls" && r.On != "commands") {
			return nil, fmt.Errorf("rule %q needs a name, match patterns and on: calls or commands", r.Name)
		}
		for _, p := range slices.Concat(r.Match, r.Not, r.Before) {
			if _, err := regexp.Compile(p); err != nil {
				return nil, fmt.Errorf("rule %q: %w", r.Name, err)
			}
		}
	}
	for _, r := range c.Fixtures.Rules {
		if _, err := regexp.Compile(r.Match); err != nil {
			return nil, fmt.Errorf("fixture: %w", err)
		}
	}
	if c.Answer != "" {
		if _, err := regexp.Compile(c.Answer); err != nil {
			return nil, fmt.Errorf("answer: %w", err)
		}
	}
	sum := sha256.Sum256(b)
	c.hash = hex.EncodeToString(sum[:8])
	return &c, nil
}
