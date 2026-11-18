package root

import (
	"regexp"
	"testing"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/build"
	"github.com/BrainerVirus/youtrack-cli/internal/skill"
	"github.com/BrainerVirus/youtrack-cli/internal/update"
)

// skillSyncOff turns the refresh off in test binaries, whose commands run
// against the real home directory unless a test opts in (SetSkillSync).
var skillSyncOff = testing.Testing()

// refreshSkills keeps the agent skill copies ytrack installed at the running
// version, so an upgrade never leaves an older skill behind. It runs before
// every command and is silent and best-effort: it never prints, and nothing
// it fails to read or write changes the command's outcome.
//
// While the registry file does not exist yet, it adopts the copies already
// installed in the home-level skills directories. When this binary is a
// release newer than the version the copies were last synced to, and no
// managed copy is newer than it, it takes the registry lock without waiting
// (skipping when another ytrack holds it) and rewrites every managed copy
// (skill.Registry.SyncNoWait): a copy another install is updating is skipped,
// not waited for, a copy the user deleted is forgotten, never re-created,
// and a copy in a repository is left for `ytrack skill install --dir`. The synced version advances only when
// every copy was rewritten. Development builds, and a release equal to or
// older than the synced one, write no copy, so two ytrack binaries on PATH
// never undo each other.
func refreshSkills(cmd *cobra.Command) {
	if skillSyncOff {
		return
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "skill", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return // skill commands manage the registry themselves
		}
	}
	reg, err := skill.OpenRegistry()
	if err != nil {
		return
	}
	if reg.Dirty() {
		_ = reg.Save()
	}
	if !syncDue(reg) {
		return
	}
	unlock, ok := skill.TryLock()
	if !ok {
		return
	}
	defer unlock()
	// Another ytrack may have synced between the first read and the lock.
	if reg, err = skill.OpenRegistry(); err != nil || !syncDue(reg) {
		return
	}
	before := reg.Clone()
	_, _ = reg.SyncNoWait(build.Version)
	if !reg.Equal(before) {
		_ = reg.Save()
	}
}

// syncDue reports whether this binary should rewrite the recorded copies: it
// is a release, newer than the version they were last synced to (with no
// stamp, newer than some copy), and no managed copy is newer than it.
func syncDue(reg *skill.Registry) bool {
	v := build.Version
	if len(reg.Dirs) == 0 || !isReleaseBuild(v) {
		return false
	}
	if update.IsRelease(reg.Synced) && !update.Newer(v, reg.Synced) {
		return false
	}
	older := 0
	for _, c := range reg.Copies(v) {
		switch {
		case !c.Managed || c.State == skill.StateMissing:
		case update.Newer(c.Version, v):
			return false // never rewrite a newer copy
		case c.Version != v:
			older++
		}
	}
	return older > 0 || update.IsRelease(reg.Synced)
}

// gitDescribe matches the suffix `git describe --tags --dirty` adds to a
// build made between releases (make build), which parses as a prerelease.
var gitDescribe = regexp.MustCompile(`-[0-9]+-g[0-9a-f]+(-dirty)?$|-dirty$`)

// isReleaseBuild reports whether v is a release version: semantic, and not a
// development build stamped by git describe.
func isReleaseBuild(v string) bool {
	return update.IsRelease(v) && !gitDescribe.MatchString(v)
}
