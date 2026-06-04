package app

import (
	"testing"

	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/store"
)

func TestMergeRankedFileMatchesPrefersCombinedScore(t *testing.T) {
	merged := mergeRankedFileMatches(
		[]store.FileMatch{
			{Path: "a.go", Score: 6, Reasons: []string{"touched in session lineage"}, Source: "session-lineage"},
			{Path: "b.go", Score: 2, Reasons: []string{"filename matches auth"}, Source: "session-lineage"},
		},
		[]store.FileMatch{
			{Path: "a.go", Score: 5, Reasons: []string{"connected by reference graph"}, Source: "repo-graph"},
			{Path: "c.go", Score: 9, Reasons: []string{"symbol name matches auth"}, Source: "repo-graph"},
		},
		10,
	)

	if len(merged) != 3 {
		t.Fatalf("expected 3 merged items, got %d", len(merged))
	}
	if merged[0].Path != "a.go" {
		t.Fatalf("expected a.go to rank first, got %s", merged[0].Path)
	}
	if merged[0].Score != 11 {
		t.Fatalf("expected a.go score 11, got %d", merged[0].Score)
	}
	if merged[0].Source != "session-lineage+repo-graph" {
		t.Fatalf("unexpected source merge: %s", merged[0].Source)
	}
	if len(merged[0].Reasons) != 2 {
		t.Fatalf("expected combined reasons, got %#v", merged[0].Reasons)
	}
}
