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
	ctx      context.Context
	db       *db.DB
	cfg      *models.Config
	program  *tea.Program
	cancels  map[string]context.CancelFunc
	mu       sync.Mutex
	mptCli   *client.MPTClient
	upCli    *client.UploadPostClient
	tgCli    *client.TelegramClient
}

func NewManager(ctx context.Context, database *db.DB, cfg *models.Config) *Manager {
	return &Manager{
		ctx:     ctx,
		db:      database,
		cfg:     cfg,
		cancels: make(map[string]context.CancelFunc),
		mptCli:  client.NewMPTClient(cfg.MPT.BaseURL),
		upCli:   client.NewUploadPostClient(&cfg.UploadPost),
		tgCli:   client.NewTelegramClient(cfg.Telegram.BotToken, cfg.Telegram.ChatID),
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

// ApproveAndPost picks up a pending_approval post and uploads it in a goroutine.
func (m *Manager) ApproveAndPost(postID string) {
	post, err := m.db.GetPost(postID)
	if err != nil || post.ID == "" {
		return
	}

	// Look up the channel to get CTA / affiliate link / platforms
	channels, _ := m.db.GetChannels()
	var ch models.Channel
	for _, c := range channels {
		if c.ID == post.ChannelID {
			ch = c
			break
		}
	}
	if ch.ID == "" {
		return
	}

	post.Status = models.PostPosting
	m.db.SavePost(post)
	m.send(post, fmt.Sprintf("📤 [%s] Aprovado — enviando para %s…", ch.Name, platformNames(ch.Platforms)))

	go func() {
		if err := m.uploadToAll(post.VideoPath, ch); err != nil {
			m.failPost(&post, ch, fmt.Sprintf("upload: %v", err))
			return
		}
		now := time.Now()
		post.Status = models.PostDone
		post.PlatformsPosted = ch.Platforms
		post.CompletedAt = &now
		m.db.SavePost(post)
		m.send(post, fmt.Sprintf("✅ [%s] Postado em %s!", ch.Name, platformNames(ch.Platforms)))
		m.telegramNotify(fmt.Sprintf("✅ <b>%s</b> postado em %s\nTópico: %s", ch.Name, platformNames(ch.Platforms), post.Topic))
	}()
}

// RejectPost marks a pending post as rejected.
func (m *Manager) RejectPost(postID string) {
	m.db.UpdatePostStatus(postID, models.PostRejected)
}

// runWorker is the main loop for a single channel.
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
			m.generateVideo(ctx, ch)
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
	return dayStart.Add(24 * time.Hour)
}

// generateVideo generates a video and sets it to pending_approval.
// It does NOT post — the user must approve from the TUI.
func (m *Manager) generateVideo(ctx context.Context, ch models.Channel) {
	post := models.Post{
		ID:          uuid.NewString(),
		ChannelID:   ch.ID,
		ChannelName: ch.Name,
		Topic:       ch.Niche,
		Status:      models.PostGenerating,
		CreatedAt:   time.Now(),
	}
	m.db.SavePost(post)
	m.send(post, fmt.Sprintf("🎬 [%s] Gerando: %s", ch.Name, ch.Niche))

	// Use per-channel language if set, otherwise fall back to global config
	lang := ch.VideoLanguage
	if lang == "" {
		lang = m.cfg.MPT.VideoLanguage
	}
	cfgOverride := m.cfg.MPT
	cfgOverride.VideoLanguage = lang

	taskID, err := m.mptCli.GenerateVideo(&cfgOverride, ch.Niche)
	if err != nil {
		m.failPost(&post, ch, fmt.Sprintf("generate: %v", err))
		return
	}
	m.send(post, fmt.Sprintf("⏳ [%s] Renderizando… (task %s)", ch.Name, taskID[:8]))

	videoPath, err := m.mptCli.WaitForTask(taskID, func(pct int) {
		m.send(post, fmt.Sprintf("⏳ [%s] %d%%", ch.Name, pct))
	})
	if err != nil {
		m.failPost(&post, ch, fmt.Sprintf("render: %v", err))
		return
	}

	post.VideoPath = videoPath
	post.Status = models.PostPendingApproval
	m.db.SavePost(post)
	m.send(post, fmt.Sprintf("⏸  [%s] Vídeo pronto — aguardando aprovação", ch.Name))
	m.telegramNotify(fmt.Sprintf(
		"⏸ <b>%s</b> — vídeo pronto para aprovação!\nTópico: %s\nArquivo: %s",
		ch.Name, ch.Niche, videoPath,
	))
}

// ── Upload helpers ────────────────────────────────────────────────────────────

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

func splitByLinkSupport(platforms []models.Platform) (noLink, withLink []models.Platform) {
	for _, p := range platforms {
		switch p {
		case models.PlatformFacebook, models.PlatformYouTube:
			withLink = append(withLink, p)
		default:
			noLink = append(noLink, p)
		}
	}
	return
}

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

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m *Manager) failPost(post *models.Post, ch models.Channel, errMsg string) {
	now := time.Now()
	post.Status = models.PostFailed
	post.Error = errMsg
	post.CompletedAt = &now
	m.db.SavePost(*post)
	m.send(*post, fmt.Sprintf("❌ [%s] Falhou: %s", ch.Name, errMsg))
	m.telegramNotify(fmt.Sprintf("❌ <b>%s</b> falhou: %s", ch.Name, errMsg))
}

func (m *Manager) send(post models.Post, log string) {
	if m.program != nil {
		m.program.Send(PostUpdateMsg{Post: post, LogLine: log})
	}
}

func (m *Manager) telegramNotify(text string) {
	if m.cfg.Telegram.Enabled {
		go m.tgCli.Send(text) // fire-and-forget
	}
}

func platformNames(platforms []models.Platform) string {
	parts := make([]string, 0, len(platforms))
	for _, p := range platforms {
		switch p {
		case models.PlatformTikTok:
			parts = append(parts, "TikTok")
		case models.PlatformInstagram:
			parts = append(parts, "IG")
		case models.PlatformFacebook:
			parts = append(parts, "FB")
		case models.PlatformYouTube:
			parts = append(parts, "YT")
		default:
			parts = append(parts, string(p))
		}
	}
	return strings.Join(parts, "+")
}
