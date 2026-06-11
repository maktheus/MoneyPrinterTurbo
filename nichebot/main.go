package main

import (
	"context"
	"fmt"
	"log"
	"nichebot/db"
	"nichebot/models"
	"nichebot/tui"
	"nichebot/worker"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

const configPath = "nichebot.toml"
const dbPath = "nichebot.db"

func main() {
	cfg, err := models.LoadConfig(configPath)
	if err != nil {
		cfg = models.DefaultConfig()
		if saveErr := cfg.Save(configPath); saveErr != nil {
			log.Fatalf("failed to create %s: %v", configPath, saveErr)
		}
		fmt.Printf("Created %s — edit it with your API keys before running.\n", configPath)
		os.Exit(0)
	}

	database, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create manager (program set after program creation to break chicken-and-egg)
	mgr := worker.NewManager(ctx, database, cfg)

	// Build TUI
	m := tui.New(database, mgr)
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Wire the program into the manager so workers can send UI updates
	mgr.SetProgram(p)

	// Start workers for all active channels persisted from previous runs
	channels, err := database.GetChannels()
	if err != nil {
		log.Fatalf("failed to load channels: %v", err)
	}
	for _, ch := range channels {
		if ch.Status == models.StatusActive {
			mgr.Start(ch)
		}
	}

	// Graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		log.Fatalf("TUI error: %v", err)
	}
}
