package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHostForPath(t *testing.T) {
	cfgWith := func(t *testing.T, paths map[string][]string) *Config {
		t.Helper()
		c, _ := LoadFrom(t.TempDir())
		for key, ps := range paths {
			c.SetHost(key, HostEntry{URL: "https://" + key, Paths: ps})
		}
		return c
	}

	t.Run("given /w/**, it matches the folder itself and every folder under it, and nothing beside it", func(t *testing.T) {
		c := cfgWith(t, map[string][]string{"work.example": {"/w/**"}})
		for dir, want := range map[string]bool{"/w": true, "/w/repo": true, "/w/repo/sub/deep": true, "/wx": false, "/other/w": false} {
			if m, ok, _ := c.HostForPath(dir); ok != want || (ok && m.Host != "work.example") {
				t.Errorf("%s: got %+v, %v, want match %v", dir, m, ok, want)
			}
		}
	})

	t.Run("given * in a glob, it matches within one folder name only", func(t *testing.T) {
		c := cfgWith(t, map[string][]string{"a.example": {"/src/*-api"}})
		if _, ok, _ := c.HostForPath("/src/billing-api"); !ok {
			t.Error("/src/billing-api did not match")
		}
		if _, ok, _ := c.HostForPath("/src/x/billing-api"); ok {
			t.Error("* crossed a folder boundary")
		}
	})

	t.Run("given nested globs on two hosts, the longest literal prefix wins", func(t *testing.T) {
		c := cfgWith(t, map[string][]string{
			"broad.example":  {"/w/**"},
			"narrow.example": {"/w/client/**"},
		})
		if m, _, _ := c.HostForPath("/w/client/repo"); m.Host != "narrow.example" || m.Glob != "/w/client/**" {
			t.Errorf("got %+v", m)
		}
		if m, _, _ := c.HostForPath("/w/other"); m.Host != "broad.example" {
			t.Errorf("got %+v", m)
		}
	})

	t.Run("given a ~ glob, it expands to the home directory", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		c := cfgWith(t, map[string][]string{"work.example": {"~/work/**"}})
		if m, ok, _ := c.HostForPath(filepath.Join(home, "work", "repo")); !ok || m.Glob != "~/work/**" {
			t.Errorf("got %+v, %v", m, ok)
		}
	})

	t.Run("given several folders, any of them can match", func(t *testing.T) {
		c := cfgWith(t, map[string][]string{"work.example": {"/real/**"}})
		if _, ok, _ := c.HostForPath("/link/repo", "/real/repo"); !ok {
			t.Error("the resolved folder did not match")
		}
	})
}

func TestHostPathEdits(t *testing.T) {
	t.Run("given an added path, it is saved in hosts.yml and survives a new login", func(t *testing.T) {
		dir := t.TempDir()
		c, _ := LoadFrom(dir)
		c.SetHost("work.example", HostEntry{URL: "https://work.example"})
		if added, err := c.AddHostPath("work.example", "/w/**"); !added || err != nil {
			t.Fatalf("added %v, %v", added, err)
		}
		if added, _ := c.AddHostPath("work.example", "/w/**"); added {
			t.Error("a duplicate was added")
		}
		c.SetHost("work.example", HostEntry{URL: "https://work.example", User: "again"})
		if err := c.Save(); err != nil {
			t.Fatal(err)
		}
		got, _ := LoadFrom(dir)
		if ps := got.HostPaths("work.example"); len(ps) != 1 || ps[0] != "/w/**" {
			t.Errorf("paths = %v", ps)
		}
		if !got.RemoveHostPath("work.example", "/w/**") || got.RemoveHostPath("work.example", "/w/**") {
			t.Error("remove did not report presence")
		}
	})

	t.Run("given an unknown host, adding a path fails", func(t *testing.T) {
		c, _ := LoadFrom(t.TempDir())
		if _, err := c.AddHostPath("nope.example", "/w/**"); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("given a relative path or an unsupported wildcard, it is refused", func(t *testing.T) {
		for _, g := range []string{"", "work/**", "/w/?", "/w/[ab]", "/w/{a,b}", "!/w/**"} {
			if err := ValidatePathGlob(g); err == nil {
				t.Errorf("%q was accepted", g)
			}
		}
		for _, g := range []string{"/w/**", "~/w/**", "/w/*/x"} {
			if err := ValidatePathGlob(g); err != nil {
				t.Errorf("%q: %v", g, err)
			}
		}
	})
}

func TestPathCase(t *testing.T) {
	cfgWith := func(t *testing.T, paths map[string][]string) *Config {
		t.Helper()
		c, _ := LoadFrom(t.TempDir())
		for key, ps := range paths {
			c.SetHost(key, HostEntry{URL: "https://" + key, Paths: ps})
		}
		return c
	}

	for goos, want := range map[string]bool{"darwin": true, "windows": true, "linux": false, "freebsd": false} {
		t.Run("given "+goos+", a folder in another case matches /Users/me/Work/** only where case does not matter", func(t *testing.T) {
			asOS(t, goos)
			c := cfgWith(t, map[string][]string{"work.example": {"/Users/me/Work/**"}})
			m, ok, err := c.HostForPath("/users/me/work/app")
			if err != nil || ok != want || (ok && m.Host != "work.example") {
				t.Errorf("got %+v, %v, %v; want match %v", m, ok, err, want)
			}
			if _, ok, _ := c.HostForPath("/Users/me/Work/app"); !ok {
				t.Error("the exact case did not match")
			}
		})
	}

	t.Run("given windows, both drive forms match C:/Code/** in any case", func(t *testing.T) {
		asOS(t, "windows")
		c := cfgWith(t, map[string][]string{"work.example": {"C:/Code/**"}})
		for _, dir := range []string{`C:\Code\x`, "c:/code/x", `c:\CODE`} {
			if m, ok, err := c.HostForPath(dir); !ok || err != nil || m.Host != "work.example" {
				t.Errorf("%s: got %+v, %v, %v", dir, m, ok, err)
			}
		}
		if _, ok, _ := c.HostForPath(`D:\Code\x`); ok {
			t.Error("another drive matched")
		}
	})

	t.Run("given linux, a drive path is not absolute and a backslash is no separator", func(t *testing.T) {
		asOS(t, "linux")
		if err := ValidatePathGlob("C:/Code/**"); err == nil {
			t.Error("C:/Code/** was accepted")
		}
		c := cfgWith(t, map[string][]string{"work.example": {"/w/**"}})
		if _, ok, _ := c.HostForPath(`/w\x`); ok {
			t.Error(`/w\x matched /w/**`)
		}
	})

	t.Run("given darwin, globs of two hosts differing only by case fail resolution naming both", func(t *testing.T) {
		asOS(t, "darwin")
		c := cfgWith(t, map[string][]string{
			"a.example": {"/w/Code/**"},
			"b.example": {"/w/code/**"},
		})
		m, ok, err := c.HostForPath("/w/code/repo")
		if err == nil || ok {
			t.Fatalf("got %+v, %v, %v; want an error", m, ok, err)
		}
		for _, s := range []string{`"/w/Code/**"`, `"/w/code/**"`, "a.example", "b.example"} {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("error lacks %s: %v", s, err)
			}
		}
		if _, ok, err := c.HostForPath("/elsewhere"); ok || err != nil {
			t.Errorf("an unmatched folder: %v, %v", ok, err)
		}
	})

	t.Run("given linux, the same two globs are different folders", func(t *testing.T) {
		asOS(t, "linux")
		c := cfgWith(t, map[string][]string{
			"a.example": {"/w/Code/**"},
			"b.example": {"/w/code/**"},
		})
		if m, ok, err := c.HostForPath("/w/code/repo"); err != nil || !ok || m.Host != "b.example" {
			t.Errorf("got %+v, %v, %v", m, ok, err)
		}
	})

	t.Run("given darwin, case variants on one host are one path", func(t *testing.T) {
		asOS(t, "darwin")
		c := cfgWith(t, map[string][]string{"a.example": {"/w/Code/**", "/w/code/**"}})
		if m, ok, err := c.HostForPath("/w/code/repo"); err != nil || !ok || m.Host != "a.example" {
			t.Errorf("got %+v, %v, %v", m, ok, err)
		}
	})
}

func TestHostPathEditsCase(t *testing.T) {
	for goos, fold := range map[string]bool{"darwin": true, "windows": true, "linux": false} {
		t.Run("given "+goos+", ~/work/** after ~/Work/** is a duplicate only where case does not matter", func(t *testing.T) {
			asOS(t, goos)
			c, _ := LoadFrom(t.TempDir())
			c.SetHost("work.example", HostEntry{URL: "https://work.example", Paths: []string{"~/Work/**"}})
			stored, found := c.FindHostPath("work.example", "~/work/**")
			if found != fold || (fold && stored != "~/Work/**") {
				t.Errorf("FindHostPath = %q, %v", stored, found)
			}
			if added, err := c.AddHostPath("work.example", "~/work/**"); err != nil || added == fold {
				t.Errorf("added %v, %v", added, err)
			}
			if removed := c.RemoveHostPath("work.example", "~/WORK/**"); removed != fold {
				t.Errorf("removed %v", removed)
			}
		})
	}
}
