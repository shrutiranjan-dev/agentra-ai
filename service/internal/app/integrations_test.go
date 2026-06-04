package app

import (
	"testing"
	"time"

	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/domain"
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

func TestActiveSummaryIndexPrefersSessionSummaryHead(t *testing.T) {
	history := []domain.Message{
		{ID: "older-summary", Parts: []domain.ContentPart{{Type: "text", Text: autoCompactPrefix + "\nold"}}},
		{ID: "msg-1", Parts: []domain.ContentPart{{Type: "text", Text: "hello"}}},
		{ID: "active-summary", Parts: []domain.ContentPart{{Type: "text", Text: autoCompactPrefix + "\nnew"}}},
	}
	session := domain.Session{SummaryMessageID: "active-summary"}
	index := activeSummaryIndex(session, history)
	if index != 2 {
		t.Fatalf("expected active summary index 2, got %d", index)
	}
}

func TestContinuationFromHistoryIncludesDepthAndCounts(t *testing.T) {
	now := time.Now().UTC()
	history := []domain.Message{
		{
			ID: "summary-1",
			Parts: []domain.ContentPart{
				{Type: "text", Text: autoCompactPrefix + "\nfirst"},
			},
			CreatedAt: now.Add(-2 * time.Hour),
		},
		{
			ID: "summary-2",
			Parts: []domain.ContentPart{
				{Type: "text", Text: autoCompactPrefix + "\nsecond"},
				{Type: "summary_parent", Text: "summary-1"},
			},
			CreatedAt: now.Add(-1 * time.Hour),
		},
		{ID: "msg-3", Parts: []domain.ContentPart{{Type: "text", Text: "recent a"}}},
		{ID: "msg-4", Parts: []domain.ContentPart{{Type: "text", Text: "recent b"}}},
	}
	session := domain.Session{
		SummaryMessageID:       "summary-2",
		SummaryParentMessageID: "summary-1",
		SummaryFromMessageID:   "msg-3",
		SummaryToMessageID:     "msg-4",
	}
	continuation := continuationFromHistory("session-1", session, history, history[1], history[2:])
	if continuation == nil {
		t.Fatalf("expected continuation state")
	}
	if continuation.SummaryDepth != 2 {
		t.Fatalf("expected depth 2, got %d", continuation.SummaryDepth)
	}
	if continuation.CompactedMessages != 2 {
		t.Fatalf("expected compacted count 2, got %d", continuation.CompactedMessages)
	}
	if continuation.RecentMessages != 2 {
		t.Fatalf("expected 2 recent messages, got %d", continuation.RecentMessages)
	}
}

func TestMessageRangeSize(t *testing.T) {
	history := []domain.Message{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
		{ID: "d"},
	}
	if got := messageRangeSize(history, "b", "d"); got != 3 {
		t.Fatalf("expected range size 3, got %d", got)
	}
	if got := messageRangeSize(history, "missing", "d"); got != 0 {
		t.Fatalf("expected missing range size 0, got %d", got)
	}
}
