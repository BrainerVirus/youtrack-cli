package config

import (
	"path/filepath"
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
			if m, ok := c.HostForPath(dir); ok != want || (ok && m.Host != "work.example") {
				t.Errorf("%s: got %+v, %v, want match %v", dir, m, ok, want)
			}
		}
	})

	t.Run("given * in a glob, it matches within one folder name only", func(t *testing.T) {
		c := cfgWith(t, map[string][]string{"a.example": {"/src/*-api"}})
		if _, ok := c.HostForPath("/src/billing-api"); !ok {
			t.Error("/src/billing-api did not match")
		}
		if _, ok := c.HostForPath("/src/x/billing-api"); ok {
			t.Error("* crossed a folder boundary")
		}
	})

	t.Run("given nested globs on two hosts, the longest literal prefix wins", func(t *testing.T) {
		c := cfgWith(t, map[string][]string{
			"broad.example":  {"/w/**"},
			"narrow.example": {"/w/client/**"},
		})
		if m, _ := c.HostForPath("/w/client/repo"); m.Host != "narrow.example" || m.Glob != "/w/client/**" {
			t.Errorf("got %+v", m)
		}
		if m, _ := c.HostForPath("/w/other"); m.Host != "broad.example" {
			t.Errorf("got %+v", m)
		}
	})

	t.Run("given a ~ glob, it expands to the home directory", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		c := cfgWith(t, map[string][]string{"work.example": {"~/work/**"}})
		if m, ok := c.HostForPath(filepath.Join(home, "work", "repo")); !ok || m.Glob != "~/work/**" {
			t.Errorf("got %+v, %v", m, ok)
		}
	})

	t.Run("given several folders, any of them can match", func(t *testing.T) {
		c := cfgWith(t, map[string][]string{"work.example": {"/real/**"}})
		if _, ok := c.HostForPath("/link/repo", "/real/repo"); !ok {
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
