package worker

import (
	"context"
	"fmt"
	"nichebot/client"
	"nichebot/db"
	"nichebot/models"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// PostUpdateMsg is sent to the TUI when a post status changes.
type PostUpdateMsg struct {
	Post    models.Post
	LogLine string
}

// Manager owns one goroutine per active channel.
type Manager struct {
	ctx     context.Context
	db      *db.DB
	cfg     *models.Config
	program *tea.Program
	cancels map[string]context.CancelFunc
	mu      sync.Mutex
	mptCli  *client.MPTClient
	upCli   *client.UploadPostClient
}

func NewManager(ctx context.Context, database *db.DB, cfg *models.Config) *Manager {
	return &Manager{
		ctx:     ctx,
		db:      database,
		cfg:     cfg,
		cancels: make(map[string]context.CancelFunc),
		mptCli:  client.NewMPTClient(cfg.MPT.BaseURL),
		upCli:   client.NewUploadPostClient(&cfg.UploadPost),
	}
}

func (m *Manager) SetProgram(p *tea.Program) {
	m.program = p
}

// Start launches a worker goroutine for the given channel if not already running.
func (m *Manager) Start(ch models.Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.cancels[ch.ID]; exists {
		return
	}
	wCtx, cancel := context.WithCancel(m.ctx)
	m.cancels[ch.ID] = cancel
	go m.runWorker(wCtx, ch)
}

// Stop cancels the worker goroutine for the given channel.
func (m *Manager) Stop(channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.cancels[channelID]; ok {
		cancel()
		delete(m.cancels, channelID)
	}
}

func (m *Manager) IsRunning(channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.cancels[channelID]
	return ok
}

// runWorker is the main loop for a single channel.
// It checks every 15 minutes whether a new post is due and generates one if so.
func (m *Manager) runWorker(ctx context.Context, ch models.Channel) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		todayCount, _ := m.db.GetTodayPostCount(ch.ID)
		expected := expectedPostsNow(ch.VideosPerDay)
		if todayCount < expected {
			m.generateAndPost(ctx, ch)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Minute):
		}
	}
}

// expectedPostsNow calculates how many posts should have happened today by now.
// Posts are spread evenly from 08:00 to 20:00.
func expectedPostsNow(videosPerDay int) int {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())
	if now.Before(dayStart) {
		return 0
	}
	window := 12 * time.Hour
	interval := window / time.Duration(videosPerDay)
	elapsed := now.Sub(dayStart)
	count := int(elapsed/interval) + 1
	if count > videosPerDay {
		count = videosPerDay
	}
	return count
}

// NextPostTime returns when the next post for this channel is due.
func NextPostTime(ch models.Channel, todayCount int) time.Time {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())
	window := 12 * time.Hour
	interval := window / time.Duration(ch.VideosPerDay)

	for i := 0; i < ch.VideosPerDay; i++ {
		postTime := dayStart.Add(interval * time.Duration(i))
		if postTime.After(now) {
			return postTime
		}
	}
	// All posts for today done — next is tomorrow at 08:00
	return dayStart.Add(24 * time.Hour)
}

func (m *Manager) generateAndPost(ctx context.Context, ch models.Channel) {
	post := models.Post{
		ID:          uuid.NewString(),
		ChannelID:   ch.ID,
		ChannelName: ch.Name,
		Topic:       ch.Niche,
		Status:      models.PostGenerating,
		CreatedAt:   time.Now(),
	}
	m.db.SavePost(post)
	m.send(post, fmt.Sprintf("🎬 [%s] Starting: %s", ch.Name, ch.Niche))

	taskID, err := m.mptCli.GenerateVideo(&m.cfg.MPT, ch.Niche)
	if err != nil {
		m.failPost(&post, ch, fmt.Sprintf("generate: %v", err))
		return
	}
	m.send(post, fmt.Sprintf("⏳ [%s] Task queued (%s…)", ch.Name, taskID[:8]))

	videoPath, err := m.mptCli.WaitForTask(taskID, func(pct int) {
		m.send(post, fmt.Sprintf("⏳ [%s] Rendering %d%%", ch.Name, pct))
	})
	if err != nil {
		m.failPost(&post, ch, fmt.Sprintf("render: %v", err))
		return
	}

	post.VideoPath = videoPath
	post.Status = models.PostPosting
	m.db.SavePost(post)
	m.send(post, fmt.Sprintf("📤 [%s] Uploading to %s…", ch.Name, platformNames(ch.Platforms)))

	if err := m.uploadToAll(videoPath, ch); err != nil {
		m.failPost(&post, ch, fmt.Sprintf("upload: %v", err))
		return
	}

	now := time.Now()
	post.Status = models.PostDone
	post.PlatformsPosted = ch.Platforms
	post.CompletedAt = &now
	m.db.SavePost(post)
	m.send(post, fmt.Sprintf("✅ [%s] Posted to %s!", ch.Name, platformNames(ch.Platforms)))
}

// uploadToAll splits platforms into two groups and posts with appropriate captions:
//   - TikTok / Instagram: CTA text only (links are not clickable there)
//   - Facebook / YouTube: CTA text + affiliate link (clickable in posts/descriptions)
func (m *Manager) uploadToAll(videoPath string, ch models.Channel) error {
	noLink, withLink := splitByLinkSupport(ch.Platforms)

	if len(noLink) > 0 {
		caption := buildCaption(ch.Niche, ch.CTAText, "")
		if err := m.upCli.Upload(videoPath, caption, noLink); err != nil {
			return fmt.Errorf("%s: %w", platformNames(noLink), err)
		}
	}
	if len(withLink) > 0 {
		caption := buildCaption(ch.Niche, ch.CTAText, ch.AffiliateLink)
		if err := m.upCli.Upload(videoPath, caption, withLink); err != nil {
			return fmt.Errorf("%s: %w", platformNames(withLink), err)
		}
	}
	return nil
}

// splitByLinkSupport separates platforms by whether they support clickable links in posts.
func splitByLinkSupport(platforms []models.Platform) (noLink, withLink []models.Platform) {
	for _, p := range platforms {
		switch p {
		case models.PlatformFacebook, models.PlatformYouTube:
			withLink = append(withLink, p)
		default: // TikTok, Instagram
			noLink = append(noLink, p)
		}
	}
	return
}

// buildCaption assembles the post caption.
// affiliateLink is only included when non-empty (FB and YT calls only).
func buildCaption(niche, cta, affiliateLink string) string {
	var b strings.Builder
	b.WriteString(niche)
	if cta != "" {
		b.WriteString("\n\n")
		b.WriteString(cta)
	}
	if affiliateLink != "" {
		b.WriteString("\n")
		b.WriteString(affiliateLink)
	}
	b.WriteString("\n\n#shorts #viral #fyp")
	return b.String()
}

func (m *Manager) failPost(post *models.Post, ch models.Channel, errMsg string) {
	now := time.Now()
	post.Status = models.PostFailed
	post.Error = errMsg
	post.CompletedAt = &now
	m.db.SavePost(*post)
	m.send(*post, fmt.Sprintf("❌ [%s] Failed: %s", ch.Name, errMsg))
}

func (m *Manager) send(post models.Post, log string) {
	if m.program != nil {
		m.program.Send(PostUpdateMsg{Post: post, LogLine: log})
	}
}

func platformNames(platforms []models.Platform) string {
	s := ""
	for i, p := range platforms {
		if i > 0 {
			s += "+"
		}
		switch p {
		case models.PlatformTikTok:
			s += "TikTok"
		case models.PlatformInstagram:
			s += "IG"
		case models.PlatformFacebook:
			s += "FB"
		case models.PlatformYouTube:
			s += "YT"
		default:
			s += string(p)
		}
	}
	return s
}
