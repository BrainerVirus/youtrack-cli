package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
)

// benchVersion is part of every result key: bump it when grading or the
// world changes, so stored results are not reused.
const benchVersion = "1"

// CaseResult is bench/results/<model>/<case>.json.
type CaseResult struct {
	Case   string      `json:"case"`
	Model  string      `json:"model"`
	Key    string      `json:"key"`
	Passes int         `json:"passes"`
	Graded int         `json:"graded"`
	Pass   bool        `json:"pass"`
	Runs   []RunResult `json:"runs"`
}

// CaseRow is one case in summary.json.
type CaseRow struct {
	Case           string   `json:"case"`
	Pass           bool     `json:"pass"`
	Passes         int      `json:"passes"`
	Graded         int      `json:"graded"`
	Blocked        int      `json:"blocked,omitempty"`
	Errors         int      `json:"errors,omitempty"`
	SkillLoaded    int      `json:"skillLoaded"`
	Failed         []string `json:"failed,omitempty"`
	MeanTurns      float64  `json:"meanTurns"`
	MeanTotalInput float64  `json:"meanTotalInput"`
	SDTotalInput   float64  `json:"sdTotalInput"`
	MeanOutput     float64  `json:"meanOutput"`
	MeanDurationMs float64  `json:"meanDurationMs"`
	CostUSD        float64  `json:"costUSD"`
}

// Summary is bench/results/<model>/summary.json.
type Summary struct {
	Model      string    `json:"model"`
	SkillHash  string    `json:"skillHash"`
	SkillBytes int       `json:"skillBytes"`
	Cases      []CaseRow `json:"cases"`
	Totals     Totals    `json:"totals"`
}

// Totals add up every graded run; TotalInput counts cached prompt tokens too.
type Totals struct {
	Cases          int     `json:"cases"`
	CasesPassed    int     `json:"casesPassed"`
	Runs           int     `json:"runs"`
	Graded         int     `json:"graded"`
	Passes         int     `json:"passes"`
	PassRate       float64 `json:"passRate"`
	Tokens         Usage   `json:"tokens"`
	TotalInput     int     `json:"totalInput"`
	MeanTotalInput float64 `json:"meanTotalInputPerRun"`
	CostUSD        float64 `json:"costUSD"`
}

func graded(r RunResult) bool { return r.Status == "pass" || r.Status == "fail" }

func newCaseResult(c *Case, model, key string, runs []RunResult) CaseResult {
	sort.Slice(runs, func(i, j int) bool { return runs[i].Run < runs[j].Run })
	cr := CaseResult{Case: c.ID, Model: model, Key: key, Runs: runs}
	for _, r := range runs {
		if graded(r) {
			cr.Graded++
			if r.Status == "pass" {
				cr.Passes++
			}
		}
	}
	cr.Pass = casePasses(cr.Passes, cr.Graded)
	return cr
}

func summarize(model string, skillHash string, skillBytes int, results []CaseResult) Summary {
	s := Summary{Model: model, SkillHash: skillHash, SkillBytes: skillBytes}
	for _, cr := range results {
		row := CaseRow{Case: cr.Case, Pass: cr.Pass, Passes: cr.Passes, Graded: cr.Graded}
		var inputs []float64
		for _, r := range cr.Runs {
			s.Totals.Runs++
			switch r.Status {
			case "blocked":
				row.Blocked++
				continue
			case "error":
				row.Errors++
				continue
			case "fail":
				row.Failed = append(row.Failed, r.Failed)
			}
			if r.SkillLoaded {
				row.SkillLoaded++
			}
			row.MeanTurns += float64(r.Turns)
			row.MeanTotalInput += float64(r.Tokens.TotalInput())
			inputs = append(inputs, float64(r.Tokens.TotalInput()))
			row.MeanDurationMs += float64(r.DurationMs)
			row.MeanOutput += float64(r.Tokens.Output)
			row.CostUSD += r.CostUSD
			s.Totals.Tokens.Input += r.Tokens.Input
			s.Totals.Tokens.Output += r.Tokens.Output
			s.Totals.Tokens.CacheCreation += r.Tokens.CacheCreation
			s.Totals.Tokens.CacheRead += r.Tokens.CacheRead
			s.Totals.CostUSD += r.CostUSD
		}
		if cr.Graded > 0 {
			n := float64(cr.Graded)
			row.MeanTurns, row.MeanTotalInput, row.MeanOutput = round(row.MeanTurns/n), round(row.MeanTotalInput/n), round(row.MeanOutput/n)
			row.MeanDurationMs = round(row.MeanDurationMs / n)
			row.SDTotalInput = round(stddev(inputs))
		}
		row.CostUSD = round4(row.CostUSD)
		s.Cases = append(s.Cases, row)
		s.Totals.Cases++
		s.Totals.Graded += cr.Graded
		s.Totals.Passes += cr.Passes
		if cr.Pass {
			s.Totals.CasesPassed++
		}
	}
	s.Totals.TotalInput = s.Totals.Tokens.TotalInput()
	if s.Totals.Graded > 0 {
		s.Totals.PassRate = round4(float64(s.Totals.Passes) / float64(s.Totals.Graded))
		s.Totals.MeanTotalInput = round(float64(s.Totals.TotalInput) / float64(s.Totals.Graded))
	}
	s.Totals.CostUSD = round4(s.Totals.CostUSD)
	return s
}

func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	mean := 0.0
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	v := 0.0
	for _, x := range xs {
		v += (x - mean) * (x - mean)
	}
	return math.Sqrt(v / float64(len(xs)-1))
}

func round(f float64) float64  { return float64(int64(f*10+0.5)) / 10 }
func round4(f float64) float64 { return float64(int64(f*10000+0.5)) / 10000 }

// writeJSON writes v scrubbed, and refuses a file that still holds a home
// path or a token.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	sc := newScrubber("")
	text := sc.scrub(string(b)) + "\n"
	if err := sc.check(text); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return os.WriteFile(path, []byte(text), 0o600)
}

func readCaseResult(path string) (CaseResult, error) {
	var cr CaseResult
	b, err := os.ReadFile(path)
	if err != nil {
		return cr, err
	}
	return cr, json.Unmarshal(b, &cr)
}

// hashTree hashes every file of a directory by relative path and content.
func hashTree(dir string) (string, int, error) {
	h := sha256.New()
	size := 0
	fsys := os.DirFS(dir)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00", p)
		h.Write(b)
		size += len(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:16], size, err
}

func readSummary(dir string) (Summary, error) {
	var s Summary
	b, err := fs.ReadFile(os.DirFS(dir), "summary.json")
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// compare prints before and after per case and reports whether any case's
// pass rate dropped.
func compare(w io.Writer, before, after Summary) bool {
	rows := map[string]CaseRow{}
	for _, r := range before.Cases {
		rows[r.Case] = r
	}
	dropped := false
	fmt.Fprintf(w, "model %s: skill %d -> %d bytes\n", after.Model, before.SkillBytes, after.SkillBytes)
	fmt.Fprintf(w, "%-22s %9s %9s %12s %12s %8s\n", "case", "before", "after", "input before", "input after", "delta")
	for _, a := range after.Cases {
		b, ok := rows[a.Case]
		if !ok {
			fmt.Fprintf(w, "%-22s %9s %4d/%-4d %12s %12.0f\n", a.Case, "new", a.Passes, a.Graded, "", a.MeanTotalInput)
			continue
		}
		mark := ""
		if rate(a.Passes, a.Graded) < rate(b.Passes, b.Graded) {
			mark, dropped = "  DROP", true
		}
		fmt.Fprintf(w, "%-22s %4d/%-4d %4d/%-4d %12.0f %12.0f %+7.1f%%%s\n", a.Case, b.Passes, b.Graded, a.Passes, a.Graded,
			b.MeanTotalInput, a.MeanTotalInput, pct(b.MeanTotalInput, a.MeanTotalInput), mark)
	}
	bt, at := before.Totals, after.Totals
	fmt.Fprintf(w, "%-22s %4d/%-4d %4d/%-4d %12.0f %12.0f %+7.1f%%\n", "total", bt.Passes, bt.Graded, at.Passes, at.Graded,
		bt.MeanTotalInput, at.MeanTotalInput, pct(bt.MeanTotalInput, at.MeanTotalInput))
	fmt.Fprintf(w, "pass rate %.1f%% -> %.1f%%, cost $%.4f -> $%.4f\n", 100*bt.PassRate, 100*at.PassRate, bt.CostUSD, at.CostUSD)
	return dropped
}

func rate(p, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(p) / float64(n)
}

func pct(before, after float64) float64 {
	if before == 0 {
		return 0
	}
	return 100 * (after - before) / before
}

func resultKey(c *Case, skillHash, model string) string {
	return strings.Join([]string{benchVersion, c.hash, skillHash, model}, "-")
}
