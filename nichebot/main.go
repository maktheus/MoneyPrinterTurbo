package main

import (
	"context"
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
	// Detect first run: config doesn't exist yet
	cfg, err := models.LoadConfig(configPath)
	firstRun := false
	if err != nil {
		cfg = models.DefaultConfig()
		firstRun = true
	}

	database, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}

	// Clean up posts that were mid-flight when the process last exited.
	database.ResetOrphanedPosts()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := worker.NewManager(ctx, database, cfg)

	// Build TUI — on first run it opens the setup wizard instead of the dashboard
	m := tui.New(database, mgr, cfg, configPath, firstRun)
	p := tea.NewProgram(m, tea.WithAltScreen())

	mgr.SetProgram(p)

	// Start workers for all active channels (no-op on first run — no channels yet)
	if !firstRun {
		channels, err := database.GetChannels()
		if err != nil {
			log.Fatalf("failed to load channels: %v", err)
		}
		for _, ch := range channels {
			if ch.Status == models.StatusActive {
				mgr.Start(ch)
			}
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
