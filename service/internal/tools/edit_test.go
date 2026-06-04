package tools

import "testing"

func TestParsePatchTargetsSingleFileFallback(t *testing.T) {
	targets, err := ParsePatchTargets("src/app.ts", "<<<<<<< SEARCH\nold\n=======\nnew\n>>>>>>> REPLACE")
	if err != nil {
		t.Fatalf("ParsePatchTargets returned error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Path != "src\\app.ts" && targets[0].Path != "src/app.ts" {
		t.Fatalf("unexpected target path: %s", targets[0].Path)
	}
	if len(targets[0].Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(targets[0].Blocks))
	}
}

func TestParsePatchTargetsMultiFile(t *testing.T) {
	patch := "*** FILE: src/a.ts\n<<<<<<< SEARCH\na\n=======\nb\n>>>>>>> REPLACE\n*** FILE: src/b.ts\n<<<<<<< SEARCH\nx\n=======\ny\n>>>>>>> REPLACE"
	targets, err := ParsePatchTargets("", patch)
	if err != nil {
		t.Fatalf("ParsePatchTargets returned error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
}

func TestParsePatchTargetsBeginPatchEnvelope(t *testing.T) {
	patch := "*** Begin Patch\n*** Update File: src/a.ts\n<<<<<<< SEARCH\nhello\n=======\nworld\n>>>>>>> REPLACE\n*** End Patch"
	targets, err := ParsePatchTargets("", patch)
	if err != nil {
		t.Fatalf("ParsePatchTargets returned error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Mode != "update" {
		t.Fatalf("expected update mode, got %s", targets[0].Mode)
	}
}

func TestParsePatchTargetsAddAndDelete(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: src/new.ts\n+line one\n+line two\n*** Delete File: src/old.ts\n*** End Patch"
	targets, err := ParsePatchTargets("", patch)
	if err != nil {
		t.Fatalf("ParsePatchTargets returned error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].Mode != "add" {
		t.Fatalf("expected add mode, got %s", targets[0].Mode)
	}
	if targets[0].NewFile != "line one\nline two" {
		t.Fatalf("unexpected add-file content: %q", targets[0].NewFile)
	}
	if targets[1].Mode != "delete" {
		t.Fatalf("expected delete mode, got %s", targets[1].Mode)
	}
}

func TestApplyPatchBlocksProducesPreview(t *testing.T) {
	result, err := ApplyPatchBlocks("hello old world", []PatchBlock{{
		Search:  "old",
		Replace: "new",
	}})
	if err != nil {
		t.Fatalf("ApplyPatchBlocks returned error: %v", err)
	}
	if result.Content != "hello new world" {
		t.Fatalf("unexpected content: %s", result.Content)
	}
	if result.Preview == "" {
		t.Fatal("expected preview to be populated")
	}
}
