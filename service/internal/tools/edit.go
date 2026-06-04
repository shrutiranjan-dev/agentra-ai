package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

type EditResult struct {
	Content      string
	Replacements int
	Preview      string
}

func ReplaceExact(original, oldText, newText string, replaceAll bool) (EditResult, error) {
	if oldText == "" {
		return EditResult{}, fmt.Errorf("old_text cannot be empty")
	}
	count := strings.Count(original, oldText)
	if count == 0 {
		return EditResult{}, fmt.Errorf("search text not found")
	}
	if !replaceAll && count > 1 {
		return EditResult{}, fmt.Errorf("search text matched %d locations; refine the edit", count)
	}
	limit := 1
	if replaceAll {
		limit = -1
	}
	return EditResult{
		Content:      strings.Replace(original, oldText, newText, limit),
		Replacements: map[bool]int{true: count, false: 1}[replaceAll],
		Preview:      formatDiffPreview("", oldText, newText, map[bool]int{true: count, false: 1}[replaceAll]),
	}, nil
}

func ApplySearchReplacePatch(original, patch string) (EditResult, error) {
	targets, err := ParsePatchTargets("", patch)
	if err != nil {
		return EditResult{}, err
	}
	if len(targets) != 1 {
		return EditResult{}, fmt.Errorf("patch must reference exactly one file in single-file mode")
	}
	return ApplyPatchBlocks(original, targets[0].Blocks)
}

func ApplyPatchBlocks(original string, blocks []PatchBlock) (EditResult, error) {
	content := original
	total := 0
	previews := make([]string, 0, len(blocks))
	for _, block := range blocks {
		result, err := ReplaceExact(content, block.Search, block.Replace, false)
		if err != nil {
			return EditResult{}, err
		}
		content = result.Content
		total += result.Replacements
		previews = append(previews, formatDiffPreview("", block.Search, block.Replace, result.Replacements))
	}
	return EditResult{
		Content:      content,
		Replacements: total,
		Preview:      strings.Join(previews, "\n\n"),
	}, nil
}

type PatchBlock struct {
	Search  string
	Replace string
}

type PatchTarget struct {
	Path      string
	Mode      string
	Blocks    []PatchBlock
	NewFile   string
	OldFile   string
	BlockText string
}

func ParsePatchTargets(defaultPath, patch string) ([]PatchTarget, error) {
	const (
		fileMarker       = "*** FILE:"
		updateFileMarker = "*** Update File:"
		addFileMarker    = "*** Add File:"
		deleteFileMarker = "*** Delete File:"
		beginMarker      = "*** Begin Patch"
		endMarker        = "*** End Patch"
	)
	normalized := strings.ReplaceAll(patch, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	hasFileMarkers := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, fileMarker) || strings.HasPrefix(trimmed, updateFileMarker) || strings.HasPrefix(trimmed, addFileMarker) || strings.HasPrefix(trimmed, deleteFileMarker) {
			hasFileMarkers = true
			break
		}
	}

	if !hasFileMarkers {
		if strings.TrimSpace(defaultPath) == "" {
			return nil, fmt.Errorf("patch is missing file markers and no target path was provided")
		}
		blocks, err := parsePatchBlocks(normalized)
		if err != nil {
			return nil, err
		}
		return []PatchTarget{{
			Path:      filepath.Clean(defaultPath),
			Mode:      "update",
			Blocks:    blocks,
			BlockText: normalized,
		}}, nil
	}

	targets := make([]PatchTarget, 0)
	var currentPath string
	var currentMode string
	var currentLines []string
	flush := func() error {
		if strings.TrimSpace(currentPath) == "" {
			return nil
		}
		body := strings.Join(currentLines, "\n")
		target := PatchTarget{
			Path:      filepath.Clean(currentPath),
			Mode:      currentMode,
			BlockText: body,
		}
		switch currentMode {
		case "add":
			target.NewFile = parseAddFileContent(body)
		case "delete":
		case "update", "":
			blocks, err := parsePatchBlocks(body)
			if err != nil {
				return fmt.Errorf("%s: %w", currentPath, err)
			}
			target.Mode = "update"
			target.Blocks = blocks
		default:
			return fmt.Errorf("%s: unsupported patch mode %s", currentPath, currentMode)
		}
		targets = append(targets, target)
		return nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == beginMarker || trimmed == endMarker {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, fileMarker):
			if err := flush(); err != nil {
				return nil, err
			}
			currentPath = strings.TrimSpace(strings.TrimPrefix(trimmed, fileMarker))
			currentMode = "update"
			currentLines = currentLines[:0]
			continue
		case strings.HasPrefix(trimmed, updateFileMarker):
			if err := flush(); err != nil {
				return nil, err
			}
			currentPath = strings.TrimSpace(strings.TrimPrefix(trimmed, updateFileMarker))
			currentMode = "update"
			currentLines = currentLines[:0]
			continue
		case strings.HasPrefix(trimmed, addFileMarker):
			if err := flush(); err != nil {
				return nil, err
			}
			currentPath = strings.TrimSpace(strings.TrimPrefix(trimmed, addFileMarker))
			currentMode = "add"
			currentLines = currentLines[:0]
			continue
		case strings.HasPrefix(trimmed, deleteFileMarker):
			if err := flush(); err != nil {
				return nil, err
			}
			currentPath = strings.TrimSpace(strings.TrimPrefix(trimmed, deleteFileMarker))
			currentMode = "delete"
			currentLines = currentLines[:0]
			continue
		}
		currentLines = append(currentLines, line)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no patch targets found")
	}
	return targets, nil
}

func parsePatchBlocks(patch string) ([]PatchBlock, error) {
	const (
		searchMarker  = "<<<<<<< SEARCH"
		splitMarker   = "======="
		replaceMarker = ">>>>>>> REPLACE"
	)
	normalized := strings.ReplaceAll(patch, "\r\n", "\n")
	parts := strings.Split(normalized, searchMarker)
	blocks := make([]PatchBlock, 0)
	for _, part := range parts[1:] {
		splitIndex := strings.Index(part, splitMarker)
		replaceIndex := strings.Index(part, replaceMarker)
		if splitIndex == -1 || replaceIndex == -1 || replaceIndex < splitIndex {
			return nil, fmt.Errorf("invalid patch block format")
		}
		search := strings.TrimPrefix(part[:splitIndex], "\n")
		replace := part[splitIndex+len(splitMarker) : replaceIndex]
		replace = strings.TrimPrefix(replace, "\n")
		blocks = append(blocks, PatchBlock{
			Search:  strings.TrimSuffix(search, "\n"),
			Replace: strings.TrimSuffix(replace, "\n"),
		})
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no patch blocks found")
	}
	return blocks, nil
}

func formatDiffPreview(path, oldText, newText string, replacements int) string {
	preview := make([]string, 0, 8)
	if strings.TrimSpace(path) != "" {
		preview = append(preview, fmt.Sprintf("--- %s", path))
		preview = append(preview, fmt.Sprintf("+++ %s", path))
	}
	preview = append(preview, fmt.Sprintf("@@ replacements: %d @@", replacements))
	for _, line := range truncatePreviewLines(oldText) {
		preview = append(preview, "- "+line)
	}
	for _, line := range truncatePreviewLines(newText) {
		preview = append(preview, "+ "+line)
	}
	return strings.Join(preview, "\n")
}

func truncatePreviewLines(text string) []string {
	const maxLines = 6
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) <= maxLines {
		return lines
	}
	return append(lines[:maxLines], "...(truncated)")
}

func parseAddFileContent(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	content := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+"):
			content = append(content, strings.TrimPrefix(line, "+"))
		case strings.TrimSpace(line) == "":
			content = append(content, "")
		}
	}
	return strings.TrimSuffix(strings.Join(content, "\n"), "\n")
}
