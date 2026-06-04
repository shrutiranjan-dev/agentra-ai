package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/app"
	"github.com/shruti-ranjan-maji/personal-assistant/service/internal/config"
)

func main() {
	prompt := flag.String("prompt", "", "Prompt to run")
	model := flag.String("model", "", "Ollama model")
	title := flag.String("title", "CLI Session", "Session title")
	flag.Parse()

	if strings.TrimSpace(*prompt) == "" {
		log.Fatal("missing -prompt")
	}

	cfg := config.Load()
	if *model == "" {
		*model = cfg.DefaultModel
	}

	ctx := context.Background()
	runtimeApp, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = runtimeApp.Close(context.Background())
	}()

	session, err := runtimeApp.CreateSession(ctx, *title)
	if err != nil {
		log.Fatal(err)
	}

	message, err := runtimeApp.RunAgent(ctx, session.ID, *model, *prompt, "")
	if err != nil {
		log.Fatal(err)
	}

	for _, part := range message.Parts {
		if part.Type == "text" {
			fmt.Println(part.Text)
		}
	}
}
