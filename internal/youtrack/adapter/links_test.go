package adapter_test

import (
	"slices"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
)

func TestFindLinkEnd(t *testing.T) {
	types := []adapter.LinkType{
		{ID: "173-0", Name: "Relates", SourceToTarget: "relates to"},
		{ID: "173-1", Name: "Depend", SourceToTarget: "is required for", TargetToSource: "depends on", Directed: true},
		{ID: "173-3", Name: "Subtask", SourceToTarget: "parent for", TargetToSource: "subtask of", Directed: true},
	}
	for phrase, want := range map[string]struct{ linkID, direction string }{
		"depends on":         {"173-1t", adapter.LinkInward},
		"  IS REQUIRED FOR ": {"173-1s", adapter.LinkOutward},
		"subtask of":         {"173-3t", adapter.LinkInward},
		"relates to":         {"173-0", adapter.LinkBoth},
		"relates":            {"173-0", adapter.LinkBoth},
	} {
		end, ok := adapter.FindLinkEnd(types, phrase)
		if !ok || end.LinkID != want.linkID || end.Direction != want.direction {
			t.Errorf("given %q, it found %+v (ok %v), want %v", phrase, end, ok, want)
		}
	}
	for _, phrase := range []string{"Depend", "Subtask", "blocks", ""} {
		if end, ok := adapter.FindLinkEnd(types, phrase); ok {
			t.Errorf("given %q, it found %+v, want no match", phrase, end)
		}
	}
	t.Run("given a phrase two types share, it matches neither", func(t *testing.T) {
		dup := slices.Concat(types, []adapter.LinkType{{ID: "173-9", Name: "Needs", SourceToTarget: "is needed by", TargetToSource: "depends on", Directed: true}})
		if end, ok := adapter.FindLinkEnd(dup, "depends on"); ok {
			t.Errorf("found %+v", end)
		}
	})
}
