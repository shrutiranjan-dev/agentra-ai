package app

import (
	"strings"
	"testing"

	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/domain"
)

func TestParseCommandTemplateMetadata(t *testing.T) {
	template, metadata := parseCommandTemplate(`---
title: Review File
description: Review a file with extra context
arg: path|required|Path to inspect
arg: focus|optional|Focus area|tests
---
Check {{path}} with focus {{focus|general}}`)

	if !strings.Contains(template, "Check {{path}}") {
		t.Fatalf("expected body template, got %q", template)
	}
	if metadata.Title != "Review File" || metadata.Description != "Review a file with extra context" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	if len(metadata.Arguments) != 2 || metadata.Arguments[0].Name != "path" || !metadata.Arguments[0].Required {
		t.Fatalf("unexpected arguments: %+v", metadata.Arguments)
	}
}

func TestRenderCommandTemplateNamedArguments(t *testing.T) {
	template := "Analyze {{path}} with focus {{focus|general}}\nAll: $ARGUMENTS"
	args := []domain.CommandArgumentDefinition{
		{Name: "path", Required: true},
		{Name: "focus", Required: false, DefaultValue: "general"},
	}

	rendered, missing := renderCommandTemplate(template, args, []string{"--path", "src/app.ts", "--focus", "auth"})
	if len(missing) != 0 {
		t.Fatalf("did not expect missing args: %v", missing)
	}
	if !strings.Contains(rendered, "Analyze src/app.ts with focus auth") {
		t.Fatalf("unexpected rendered template: %s", rendered)
	}
}

func TestRenderCommandTemplateReportsMissingRequired(t *testing.T) {
	template := "Analyze {{path}}"
	args := []domain.CommandArgumentDefinition{{Name: "path", Required: true}}

	_, missing := renderCommandTemplate(template, args, []string{"--focus", "auth"})
	if len(missing) != 1 || missing[0] != "path" {
		t.Fatalf("expected missing path, got %v", missing)
	}
}
