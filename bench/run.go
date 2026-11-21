package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// allowedTools is what the model may use: the skill, reading files in the
// project, and Bash for ytrack plus a few harmless commands. Everything else
// is denied (dontAsk), so a run cannot reach the network, a real ytrack or
// files outside its world by another route.
var allowedTools = []string{
	"Skill", "Read(./**)",
	"Bash(ytrack *)", "Bash(ls *)", "Bash(jq *)", "Bash(wc *)", "Bash(echo *)", "Bash(pwd)",
}

var disallowedTools = []string{"WebFetch", "WebSearch", "Task", "Agent"}

// Usage is the token count Claude Code reports for a run.
type Usage struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheCreation int `json:"cacheCreation"`
	CacheRead     int `json:"cacheRead"`
}

// TotalInput is every prompt token, cached or not.
func (u Usage) TotalInput() int { return u.Input + u.CacheCreation + u.CacheRead }

// RunResult is one graded run of a case.
type RunResult struct {
	Run int `json:"run"`
	// Status is pass, fail, blocked (a host policy denied a plain ytrack
	// command; not graded) or error (the harness failed; not graded).
	Status      string   `json:"status"`
	Failed      string   `json:"failed,omitempty"`
	Failures    []string `json:"failures,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	Turns       int      `json:"turns"`
	Tokens      Usage    `json:"tokens"`
	CostUSD     float64  `json:"costUSD"`
	DurationMs  int64    `json:"durationMs"`
	SkillLoaded bool     `json:"skillLoaded"`
	// SkillListed: the init event lists the ytrack skill; WorkitPlugin: it
	// lists the Workit plugin (it should not, the run drops user plugins).
	SkillListed  bool     `json:"skillListed"`
	WorkitPlugin bool     `json:"workitPlugin,omitempty"`
	Commands     []string `json:"commands"`
	Calls        []Call   `json:"calls"`
	Denials      []string `json:"denials,omitempty"`
	Answer       string   `json:"answer"`
}

type runner struct {
	claude  string
	model   string
	skill   string // "" for the no-skill control
	fake    string // this executable, copied in as <harness>/bin/ytrack
	timeout time.Duration
	keep    bool
	logDir  string // scrubbed transcripts, not committed
}

// runCase runs one case once in a fresh world and grades it.
func (r *runner) runCase(ctx context.Context, c *Case, n int) (res RunResult) {
	res = RunResult{Run: n}
	world, err := os.MkdirTemp("", "ytrack-bench-")
	if err != nil {
		return errResult(res, err)
	}
	if !r.keep {
		defer os.RemoveAll(world)
	}
	project := filepath.Join(world, "project")
	harness := filepath.Join(world, "harness")
	if err := r.makeWorld(c, project, harness); err != nil {
		return errResult(res, err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []string{
		"-p", "--model", r.model, "--output-format", "stream-json", "--verbose",
		"--max-turns", fmt.Sprint(c.MaxTurns),
		// Only the world's project settings: no user plugins or hooks (Workit's
		// gate included), no user skills, no MCP servers.
		"--setting-sources", "project", "--strict-mcp-config", "--no-session-persistence",
		"--permission-mode", "dontAsk", "--max-budget-usd", "0.50",
		"--allowedTools",
	}
	args = append(args, allowedTools...)
	args = append(args, "--disallowedTools")
	args = append(args, disallowedTools...)
	cmd := exec.CommandContext(ctx, r.claude, args...) //nolint:gosec // the claude binary and flags are the bench's own
	cmd.Dir = project
	cmd.Env = childEnv(os.Environ(), harness)
	cmd.Stdin = strings.NewReader(c.Prompt)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	start := time.Now()
	runErr := cmd.Run()
	res.DurationMs = time.Since(start).Milliseconds()

	sc := newScrubber(world)
	if r.logDir != "" {
		_ = os.MkdirAll(r.logDir, 0o750)
		_ = os.WriteFile(filepath.Join(r.logDir, fmt.Sprintf("%s.run%d.jsonl", c.ID, n)), []byte(sc.scrub(out.String())), 0o600)
	}
	t, err := parseTranscript(out.Bytes())
	if err != nil || !t.done {
		reason := fmt.Sprintf("no result event (exit: %v): %s", runErr, lastLine(stderr.String()))
		if err != nil {
			reason = err.Error()
		}
		res.Reason = sc.scrub(reason)
		res.Status = "error"
		return res
	}
	calls, err := readCalls(filepath.Join(harness, "calls.jsonl"))
	if err != nil {
		return errResult(res, err)
	}
	res.Turns, res.Tokens, res.CostUSD, res.SkillLoaded = t.turns, t.usage, t.cost, t.skillLoaded
	res.SkillListed, res.WorkitPlugin = t.skillListed, t.workitPlugin
	res.Commands, res.Calls, res.Denials, res.Answer = t.commands, calls, t.denials, t.answer
	defer func() { scrubRun(sc, &res) }()

	switch {
	case r.skill != "" && !t.skillListed:
		res.Status, res.Reason = "error", "the ytrack skill is not in the init event's skills"
	case r.skill == "" && t.skillListed:
		res.Status, res.Reason = "error", "the no-skill control lists a ytrack skill"
	case r.skill != "" && !t.skillLoaded:
		// The case measures the skill; a run that never read it measures
		// nothing (it is counted under notLoaded, not graded).
		res.Status, res.Reason = "error", notLoaded
	case t.isError && t.subtype != "error_max_turns":
		res.Status, res.Reason = "error", "claude: "+t.subtype
	case hostBlocked(t.denials):
		res.Status, res.Reason = "blocked", "a host policy denied a plain ytrack command"
	case ranYtrack(t.commands, t.denials) && len(calls) == 0:
		res.Status, res.Reason = "error", "ytrack ran but the fake logged nothing: a real ytrack may be first on PATH"
	default:
		res.Failures = Grade(c, calls, t.commands, t.answer)
		if t.subtype == "error_max_turns" {
			res.Failures = append(res.Failures, "max turns")
		}
		res.Status = "pass"
		if len(res.Failures) > 0 {
			res.Status, res.Failed = "fail", res.Failures[0]
		}
	}
	return res
}

// scrubRun replaces the world's paths and any token in what a run recorded.
func scrubRun(sc *scrubber, res *RunResult) {
	for i, c := range res.Commands {
		res.Commands[i] = sc.scrub(c)
	}
	for i, d := range res.Denials {
		res.Denials[i] = sc.scrub(d)
	}
	for _, c := range res.Calls {
		for j, a := range c.Argv {
			c.Argv[j] = sc.scrub(a)
		}
	}
	res.Answer, res.Reason = sc.scrub(res.Answer), sc.scrub(res.Reason)
}

// notLoaded is the reason of a with-skill run whose model never loaded the skill.
const notLoaded = "the model did not load the ytrack skill"

func errResult(res RunResult, err error) RunResult {
	res.Status, res.Reason = "error", err.Error()
	return res
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func (r *runner) makeWorld(c *Case, project, harness string) error {
	for _, d := range []string{filepath.Join(project, ".claude", "skills"), filepath.Join(harness, "bin"), filepath.Join(harness, "home")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
	}
	if r.skill != "" {
		if err := copyTree(r.skill, filepath.Join(project, ".claude", "skills", "ytrack")); err != nil {
			return err
		}
	}
	for name, body := range c.Files {
		p := filepath.Join(project, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			return err
		}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(harness, "case.json"), b, 0o600); err != nil {
		return err
	}
	return copyFile(r.fake, filepath.Join(harness, "bin", "ytrack"), 0o700)
}

// childEnv is the parent environment without Workit's and ytrack's own
// variables (so a run never touches the calling session or a real host),
// with the fake first on PATH and ytrack's config and cache in the harness.
func childEnv(parent []string, harness string) []string {
	var env []string
	for _, kv := range parent {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "WORKIT_"), strings.HasPrefix(k, "YTRACK_"),
			k == "CLAUDECODE", k == "ANTHROPIC_API_KEY", k == "ANTHROPIC_AUTH_TOKEN":
			continue
		case k == "PATH":
			kv = "PATH=" + filepath.Join(harness, "bin") + string(os.PathListSeparator) + v
		}
		env = append(env, kv)
	}
	home := filepath.Join(harness, "home")
	return append(env,
		"YTRACK_CONFIG_DIR="+filepath.Join(home, "config"),
		"YTRACK_CACHE_DIR="+filepath.Join(home, "cache"),
		"YTRACK_NO_UPDATE_NOTIFIER=1",
	)
}

type transcript struct {
	done, isError, skillLoaded bool
	skillListed, workitPlugin  bool
	subtype, answer            string
	turns                      int
	cost                       float64
	usage                      Usage
	commands, denials          []string
}

func parseTranscript(b []byte) (transcript, error) {
	var t transcript
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var ev struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Skills  []string `json:"skills"`
			Plugins []struct {
				Name string `json:"name"`
			} `json:"plugins"`
			Result            string  `json:"result"`
			IsError           bool    `json:"is_error"`
			NumTurns          int     `json:"num_turns"`
			TotalCostUSD      float64 `json:"total_cost_usd"`
			PermissionDenials []struct {
				ToolName  string         `json:"tool_name"`
				ToolInput map[string]any `json:"tool_input"`
			} `json:"permission_denials"`
			Usage struct {
				Input         int `json:"input_tokens"`
				Output        int `json:"output_tokens"`
				CacheCreation int `json:"cache_creation_input_tokens"`
				CacheRead     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "system":
			if ev.Subtype == "init" {
				t.skillListed = slices.Contains(ev.Skills, "ytrack")
				for _, p := range ev.Plugins {
					t.workitPlugin = t.workitPlugin || strings.Contains(p.Name, "workit")
				}
			}
		case "assistant":
			var blocks []struct {
				Type  string         `json:"type"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			}
			_ = json.Unmarshal(ev.Message.Content, &blocks)
			for _, bl := range blocks {
				if bl.Type != "tool_use" {
					continue
				}
				switch bl.Name {
				case "Bash":
					t.commands = append(t.commands, fmt.Sprint(bl.Input["command"]))
				case "Skill":
					t.skillLoaded = t.skillLoaded || strings.Contains(fmt.Sprint(bl.Input["skill"]), "ytrack")
				case "Read":
					t.skillLoaded = t.skillLoaded || strings.Contains(fmt.Sprint(bl.Input["file_path"]), "skills/ytrack/SKILL.md")
				}
			}
		case "result":
			t.done, t.isError, t.subtype, t.answer = true, ev.IsError, ev.Subtype, ev.Result
			t.turns, t.cost = ev.NumTurns, ev.TotalCostUSD
			t.usage = Usage{ev.Usage.Input, ev.Usage.Output, ev.Usage.CacheCreation, ev.Usage.CacheRead}
			for _, d := range ev.PermissionDenials {
				if c, ok := d.ToolInput["command"]; ok {
					t.denials = append(t.denials, fmt.Sprint(c))
				} else {
					t.denials = append(t.denials, d.ToolName)
				}
			}
		}
	}
	return t, sc.Err()
}

// plainYtrack is a single ytrack command with nothing the allow rule
// Bash(ytrack *) could refuse: no pipe, chain, redirect or expansion.
var plainYtrack = regexp.MustCompile("^\\s*ytrack\\s[^|;&<>$`\\n]*$")

// hostBlocked: the bench allows a plain ytrack command, so its denial came
// from a host policy (a hook or managed setting), not from the skill.
func hostBlocked(denials []string) bool {
	for _, d := range denials {
		if plainYtrack.MatchString(d) {
			return true
		}
	}
	return false
}

var ytrackWord = regexp.MustCompile(`(^|[\s;&|(])ytrack\s`)

func ranYtrack(commands, denials []string) bool {
	denied := map[string]bool{}
	for _, d := range denials {
		denied[d] = true
	}
	for _, c := range commands {
		if !denied[c] && ytrackWord.MatchString(c) {
			return true
		}
	}
	return false
}

func readCalls(path string) ([]Call, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var calls []Call
	for _, l := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		var c Call
		if json.Unmarshal(l, &c) == nil {
			calls = append(calls, c)
		}
	}
	return calls, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		return copyFile(p, target, 0o600)
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
