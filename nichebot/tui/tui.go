package tui

import (
	"fmt"
	"nichebot/db"
	"nichebot/models"
	"nichebot/worker"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	purple    = lipgloss.Color("#7D56F4")
	green     = lipgloss.Color("#04B575")
	red       = lipgloss.Color("#FF5F87")
	blue      = lipgloss.Color("#6C91BF")
	dark      = lipgloss.Color("#3C3C3C")
	muted     = lipgloss.Color("#626262")
	white     = lipgloss.Color("#FFFFFF")
	yellow    = lipgloss.Color("#FFD080")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(purple).Padding(0, 1)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(white)
	activeStyle = lipgloss.NewStyle().Foreground(green)
	pausedStyle = lipgloss.NewStyle().Foreground(red)
	dimStyle    = lipgloss.NewStyle().Foreground(muted)
	logStyle    = lipgloss.NewStyle().Foreground(blue)
	errStyle    = lipgloss.NewStyle().Foreground(red)
	okStyle     = lipgloss.NewStyle().Foreground(green)
	warnStyle   = lipgloss.NewStyle().Foreground(yellow)
	helpStyle   = lipgloss.NewStyle().Foreground(muted)
	docStyle    = lipgloss.NewStyle().Margin(1, 2)

	focusedBorder = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(purple)
	blurredBorder = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(dark)

	tableStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(dark)
)

// ── View states ───────────────────────────────────────────────────────────────

type viewState int

const (
	viewDashboard viewState = iota
	viewAddChannel
)

// ── Messages ──────────────────────────────────────────────────────────────────

type tickMsg time.Time

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	view    viewState
	db      *db.DB
	manager *worker.Manager

	// Dashboard
	channels []models.Channel
	posts    []models.Post
	table    table.Model
	logs     []string

	// Add-channel form
	inputs      [3]textinput.Model
	focusIdx    int
	platforms   map[models.Platform]bool
	platList    []models.Platform
	platFocus   int
	inPlatforms bool
	formErr     string

	width  int
	height int
}

func New(database *db.DB, mgr *worker.Manager) Model {
	// Text inputs: name, niche, videos/day
	name := textinput.New()
	name.Placeholder = "CatLover"
	name.CharLimit = 50
	name.Width = 28
	name.Focus()

	niche := textinput.New()
	niche.Placeholder = "funny cat videos"
	niche.CharLimit = 100
	niche.Width = 36

	count := textinput.New()
	count.Placeholder = "3"
	count.CharLimit = 2
	count.Width = 4
	count.SetValue("3")

	// Channels table
	cols := []table.Column{
		{Title: "Channel", Width: 14},
		{Title: "Niche", Width: 20},
		{Title: "Platforms", Width: 11},
		{Title: "Today", Width: 7},
		{Title: "Total", Width: 7},
		{Title: "Fails", Width: 6},
		{Title: "Next Post", Width: 12},
		{Title: "•", Width: 2},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(8),
	)
	ts := table.DefaultStyles()
	ts.Header = ts.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(dark).
		BorderBottom(true).
		Bold(true)
	ts.Selected = ts.Selected.
		Foreground(white).
		Background(purple).
		Bold(true)
	t.SetStyles(ts)

	m := Model{
		view:    viewDashboard,
		db:      database,
		manager: mgr,
		inputs:  [3]textinput.Model{name, niche, count},
		platforms: map[models.Platform]bool{
			models.PlatformTikTok:    true,
			models.PlatformInstagram: true,
			models.PlatformFacebook:  true,
			models.PlatformYouTube:   false,
		},
		platList: []models.Platform{
			models.PlatformTikTok,
			models.PlatformInstagram,
			models.PlatformFacebook,
			models.PlatformYouTube,
		},
		table: t,
	}
	m.refresh()
	return m
}

// ── Bubbletea interface ───────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tickEvery(30*time.Second))
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.refresh()
		return m, tickEvery(30 * time.Second)

	case worker.PostUpdateMsg:
		m.addLog(msg.LogLine)
		m.refresh()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) View() string {
	switch m.view {
	case viewDashboard:
		return m.renderDashboard()
	case viewAddChannel:
		return m.renderAddChannel()
	}
	return ""
}

// ── Key handling ──────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.view == viewAddChannel {
		return m.handleFormKey(msg)
	}
	return m.handleDashKey(msg)
}

func (m Model) handleDashKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "a":
		m.view = viewAddChannel
		m.resetForm()
		return m, textinput.Blink

	case "p":
		if row := m.selectedChannel(); row != nil {
			if row.Status == models.StatusActive {
				m.db.UpdateChannelStatus(row.ID, models.StatusPaused)
				if m.manager != nil {
					m.manager.Stop(row.ID)
				}
				m.addLog(fmt.Sprintf("⏸  [%s] paused", row.Name))
			} else {
				m.db.UpdateChannelStatus(row.ID, models.StatusActive)
				if m.manager != nil {
					m.manager.Start(*row)
				}
				m.addLog(fmt.Sprintf("▶  [%s] resumed", row.Name))
			}
			m.refresh()
		}

	case "D":
		if row := m.selectedChannel(); row != nil {
			if m.manager != nil {
				m.manager.Stop(row.ID)
			}
			m.db.DeleteChannel(row.ID)
			m.addLog(fmt.Sprintf("🗑  [%s] deleted", row.Name))
			m.refresh()
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewDashboard
		return m, nil

	case "ctrl+s", "ctrl+enter":
		return m.submitForm()

	case "tab", "down":
		if m.inPlatforms {
			m.platFocus = (m.platFocus + 1) % len(m.platList)
		} else if m.focusIdx < 2 {
			m.focusIdx++
			m.syncFocus()
		} else {
			m.inPlatforms = true
			m.platFocus = 0
			m.inputs[m.focusIdx].Blur()
		}
		return m, textinput.Blink

	case "shift+tab", "up":
		if m.inPlatforms {
			if m.platFocus > 0 {
				m.platFocus--
			} else {
				m.inPlatforms = false
				m.focusIdx = 2
				m.syncFocus()
			}
		} else if m.focusIdx > 0 {
			m.focusIdx--
			m.syncFocus()
		}
		return m, textinput.Blink

	case "enter", " ":
		if m.inPlatforms {
			p := m.platList[m.platFocus]
			m.platforms[p] = !m.platforms[p]
			return m, nil
		}
		if m.focusIdx < 2 {
			m.focusIdx++
			m.syncFocus()
			return m, textinput.Blink
		}
		// last input -> move to platforms
		m.inPlatforms = true
		m.platFocus = 0
		m.inputs[m.focusIdx].Blur()
		return m, nil
	}

	// Route keystrokes to the focused text input
	if !m.inPlatforms {
		var cmd tea.Cmd
		m.inputs[m.focusIdx], cmd = m.inputs[m.focusIdx].Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── Form submission ───────────────────────────────────────────────────────────

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.inputs[0].Value())
	niche := strings.TrimSpace(m.inputs[1].Value())
	countStr := strings.TrimSpace(m.inputs[2].Value())

	if name == "" || niche == "" {
		m.formErr = "Channel name and niche are required"
		return m, nil
	}

	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 || count > 10 {
		count = 3
	}

	var chosen []models.Platform
	for _, p := range m.platList {
		if m.platforms[p] {
			chosen = append(chosen, p)
		}
	}
	if len(chosen) == 0 {
		m.formErr = "Select at least one platform"
		return m, nil
	}

	ch := models.Channel{
		ID:           uuid.NewString(),
		Name:         name,
		Niche:        niche,
		Platforms:    chosen,
		VideosPerDay: count,
		Status:       models.StatusActive,
		CreatedAt:    time.Now(),
	}
	if err := m.db.SaveChannel(ch); err != nil {
		m.formErr = fmt.Sprintf("DB error: %v", err)
		return m, nil
	}
	if m.manager != nil {
		m.manager.Start(ch)
	}

	m.addLog(fmt.Sprintf("✨ Channel '%s' created (%s, %d/day)", name, niche, count))
	m.view = viewDashboard
	m.formErr = ""
	m.refresh()
	return m, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m *Model) refresh() {
	channels, _ := m.db.GetChannels()
	m.channels = channels
	posts, _ := m.db.GetRecentPosts(20)
	m.posts = posts
	m.rebuildTable()
}

func (m *Model) rebuildTable() {
	rows := make([]table.Row, len(m.channels))
	for i, ch := range m.channels {
		stats, _ := m.db.GetChannelStats(ch.ID)
		status := activeStyle.Render("●")
		if ch.Status == models.StatusPaused {
			status = pausedStyle.Render("○")
		}
		rows[i] = table.Row{
			ch.Name,
			truncStr(ch.Niche, 20),
			platIcons(ch.Platforms),
			fmt.Sprintf("%d/%d", stats.TodayCount, ch.VideosPerDay),
			strconv.Itoa(stats.TotalCount),
			strconv.Itoa(stats.FailCount),
			nextPost(ch, stats.TodayCount),
			status,
		}
	}
	m.table.SetRows(rows)
}

func (m *Model) addLog(line string) {
	ts := time.Now().Format("15:04")
	entry := fmt.Sprintf("[%s] %s", ts, line)
	m.logs = append(m.logs, entry)
	if len(m.logs) > 60 {
		m.logs = m.logs[len(m.logs)-60:]
	}
}

func (m *Model) resetForm() {
	m.inputs[0].SetValue("")
	m.inputs[1].SetValue("")
	m.inputs[2].SetValue("3")
	m.focusIdx = 0
	m.syncFocus()
	m.inPlatforms = false
	m.platFocus = 0
	m.platforms = map[models.Platform]bool{
		models.PlatformTikTok:    true,
		models.PlatformInstagram: true,
		models.PlatformFacebook:  true,
		models.PlatformYouTube:   false,
	}
	m.formErr = ""
}

func (m *Model) syncFocus() {
	for i := range m.inputs {
		if i == m.focusIdx {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
}

func (m *Model) selectedChannel() *models.Channel {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.channels) {
		return nil
	}
	ch := m.channels[idx]
	return &ch
}

// ── Render: Dashboard ─────────────────────────────────────────────────────────

func (m Model) renderDashboard() string {
	var b strings.Builder

	// Header
	title := titleStyle.Render("◆ NicheBot")
	help := helpStyle.Render("[a] Add  [p] Pause/Resume  [D] Delete  [↑↓] Navigate  [q] Quit")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", help))
	b.WriteString("\n\n")

	// Channels table
	b.WriteString(headerStyle.Render("CHANNELS"))
	b.WriteString("\n")
	if len(m.channels) == 0 {
		b.WriteString(dimStyle.Render("  No channels yet. Press [a] to add one.\n"))
	} else {
		b.WriteString(tableStyle.Render(m.table.View()))
	}

	// Legend
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  ● active  ○ paused   Today = posts done today / daily target"))
	b.WriteString("\n\n")

	// Recent activity log
	b.WriteString(headerStyle.Render("ACTIVITY LOG"))
	b.WriteString("\n")
	if len(m.logs) == 0 {
		b.WriteString(dimStyle.Render("  Waiting for workers…\n"))
	} else {
		visible := m.logs
		if len(visible) > 12 {
			visible = visible[len(visible)-12:]
		}
		for _, line := range visible {
			b.WriteString(styledLog(line))
			b.WriteString("\n")
		}
	}

	return docStyle.Render(b.String())
}

func styledLog(line string) string {
	switch {
	case strings.Contains(line, "✅"):
		return "  " + okStyle.Render(line)
	case strings.Contains(line, "❌"):
		return "  " + errStyle.Render(line)
	case strings.Contains(line, "⏳"):
		return "  " + warnStyle.Render(line)
	case strings.Contains(line, "⏸"), strings.Contains(line, "▶"), strings.Contains(line, "🗑"), strings.Contains(line, "✨"):
		return "  " + dimStyle.Render(line)
	default:
		return "  " + logStyle.Render(line)
	}
}

// ── Render: Add Channel form ──────────────────────────────────────────────────

func (m Model) renderAddChannel() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("◆ Add Channel"))
	b.WriteString("\n\n")

	labels := []string{"Channel Name", "Niche / Topic", "Videos per day"}
	for i, inp := range m.inputs {
		focused := i == m.focusIdx && !m.inPlatforms
		if focused {
			b.WriteString(activeStyle.Render("▸ " + labels[i]))
		} else {
			b.WriteString(dimStyle.Render("  " + labels[i]))
		}
		b.WriteString("\n")

		view := inp.View()
		if focused {
			b.WriteString("  " + focusedBorder.Render(view))
		} else {
			b.WriteString("  " + blurredBorder.Render(view))
		}
		b.WriteString("\n\n")
	}

	// Platforms
	b.WriteString("\n")
	if m.inPlatforms {
		b.WriteString(activeStyle.Render("▸ Platforms"))
	} else {
		b.WriteString(dimStyle.Render("  Platforms"))
	}
	b.WriteString("\n  ")

	for i, p := range m.platList {
		checked := "[ ] "
		if m.platforms[p] {
			checked = "[✓] "
		}
		label := checked + platName(p)
		switch {
		case m.inPlatforms && i == m.platFocus:
			b.WriteString(activeStyle.Render(label))
		case m.platforms[p]:
			b.WriteString(okStyle.Render(label))
		default:
			b.WriteString(dimStyle.Render(label))
		}
		b.WriteString("   ")
	}
	b.WriteString("\n")

	if m.formErr != "" {
		b.WriteString("\n" + errStyle.Render("  ⚠ "+m.formErr) + "\n")
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("[Tab/↓] Next  [Shift+Tab/↑] Prev  [Space/Enter] Toggle  [Ctrl+S] Create  [Esc] Cancel"))

	return docStyle.Render(b.String())
}

// ── Pure helpers ──────────────────────────────────────────────────────────────

func platIcons(platforms []models.Platform) string {
	var parts []string
	for _, p := range platforms {
		switch p {
		case models.PlatformTikTok:
			parts = append(parts, "TT")
		case models.PlatformInstagram:
			parts = append(parts, "IG")
		case models.PlatformFacebook:
			parts = append(parts, "FB")
		case models.PlatformYouTube:
			parts = append(parts, "YT")
		}
	}
	return strings.Join(parts, " ")
}

func platName(p models.Platform) string {
	switch p {
	case models.PlatformTikTok:
		return "TikTok"
	case models.PlatformInstagram:
		return "Instagram"
	case models.PlatformFacebook:
		return "Facebook"
	case models.PlatformYouTube:
		return "YouTube"
	}
	return string(p)
}

func nextPost(ch models.Channel, todayCount int) string {
	if ch.Status == models.StatusPaused {
		return pausedStyle.Render("paused")
	}
	t := worker.NextPostTime(ch, todayCount)
	dur := time.Until(t)
	if dur <= 0 {
		return warnStyle.Render("now")
	}
	h := int(dur.Hours())
	min := int(dur.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("in %dh%02dm", h, min)
	}
	return fmt.Sprintf("in %dm", min)
}

func truncStr(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}
