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
	purple = lipgloss.Color("#7D56F4")
	green  = lipgloss.Color("#04B575")
	red    = lipgloss.Color("#FF5F87")
	blue   = lipgloss.Color("#6C91BF")
	dark   = lipgloss.Color("#3C3C3C")
	muted  = lipgloss.Color("#626262")
	white  = lipgloss.Color("#FFFFFF")
	yellow = lipgloss.Color("#FFD080")

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
	tableStyle    = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(dark)

	infoBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dark).
		Padding(0, 2).
		Foreground(lipgloss.Color("#AAAAAA"))

	selectedMenuStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(white).
				Background(purple).
				Padding(0, 1)

	menuStyle = lipgloss.NewStyle().
			Foreground(muted).
			Padding(0, 1)
)

// ── View states ───────────────────────────────────────────────────────────────

type viewState int

const (
	viewSetup      viewState = iota // first-run wizard
	viewDashboard
	viewAddChannel
)

// setup sub-steps
const (
	setupWelcome    = 0
	setupMPT        = 1
	setupUploadPost = 2
	setupDone       = 3
)

// welcome menu choices
const (
	choiceFull   = 0
	choiceMPTOnly = 1
	choiceSkip   = 2
)

// ── Messages ──────────────────────────────────────────────────────────────────

type tickMsg time.Time

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	view    viewState
	db      *db.DB
	manager *worker.Manager
	cfg     *models.Config
	cfgPath string

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

	// Setup wizard
	setupStep      int
	setupChoice    int                // welcome menu selection
	setupMPTInputs [3]textinput.Model // [0]=url [1]=voice [2]=language
	setupUPInputs  [2]textinput.Model // [0]=api_key [1]=username
	setupFocusIdx  int
	setupErr       string

	width  int
	height int
}

func New(database *db.DB, mgr *worker.Manager, cfg *models.Config, cfgPath string, firstRun bool) Model {
	startView := viewDashboard
	if firstRun {
		startView = viewSetup
	}

	// Channel form inputs
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

	// Setup: MPT inputs
	mptURL := textinput.New()
	mptURL.Placeholder = "http://localhost:8080"
	mptURL.Width = 38
	mptURL.CharLimit = 100
	mptURL.SetValue(cfg.MPT.BaseURL)
	mptURL.Focus()

	mptVoice := textinput.New()
	mptVoice.Placeholder = "en-US-JennyNeural-Female"
	mptVoice.Width = 38
	mptVoice.CharLimit = 100
	mptVoice.SetValue(cfg.MPT.VoiceName)

	mptLang := textinput.New()
	mptLang.Placeholder = "en"
	mptLang.Width = 10
	mptLang.CharLimit = 10
	mptLang.SetValue(cfg.MPT.VideoLanguage)

	// Setup: Upload-Post inputs
	upKey := textinput.New()
	upKey.Placeholder = "sua-api-key-aqui"
	upKey.Width = 38
	upKey.CharLimit = 200
	upKey.EchoMode = textinput.EchoPassword
	upKey.SetValue(cfg.UploadPost.APIKey)
	upKey.Focus()

	upUser := textinput.New()
	upUser.Placeholder = "seu-username"
	upUser.Width = 38
	upUser.CharLimit = 100
	upUser.SetValue(cfg.UploadPost.Username)

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
		view:    startView,
		db:      database,
		manager: mgr,
		cfg:     cfg,
		cfgPath: cfgPath,
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
		table:          t,
		setupStep:      setupWelcome,
		setupChoice:    choiceFull,
		setupMPTInputs: [3]textinput.Model{mptURL, mptVoice, mptLang},
		setupUPInputs:  [2]textinput.Model{upKey, upUser},
	}
	if !firstRun {
		m.refresh()
	}
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
		if m.view == viewDashboard {
			m.refresh()
		}
		return m, tickEvery(30 * time.Second)

	case worker.PostUpdateMsg:
		m.addLog(msg.LogLine)
		if m.view == viewDashboard {
			m.refresh()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) View() string {
	switch m.view {
	case viewSetup:
		return m.renderSetup()
	case viewDashboard:
		return m.renderDashboard()
	case viewAddChannel:
		return m.renderAddChannel()
	}
	return ""
}

// ── Key handling ──────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewSetup:
		return m.handleSetupKey(msg)
	case viewAddChannel:
		return m.handleFormKey(msg)
	default:
		return m.handleDashKey(msg)
	}
}

// ── Setup wizard keys ─────────────────────────────────────────────────────────

func (m Model) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.setupStep {
	case setupWelcome:
		return m.handleSetupWelcomeKey(msg)
	case setupMPT:
		return m.handleSetupMPTKey(msg)
	case setupUploadPost:
		return m.handleSetupUPKey(msg)
	case setupDone:
		return m.handleSetupDoneKey(msg)
	}
	return m, nil
}

func (m Model) handleSetupWelcomeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.setupChoice > 0 {
			m.setupChoice--
		}
	case "down", "j":
		if m.setupChoice < 2 {
			m.setupChoice++
		}
	case "enter", " ":
		switch m.setupChoice {
		case choiceSkip:
			m.cfg.Save(m.cfgPath)
			m.view = viewDashboard
			m.refresh()
			return m, nil
		default:
			m.setupStep = setupMPT
			m.setupFocusIdx = 0
			m.syncSetupMPTFocus()
			return m, textinput.Blink
		}
	}
	return m, nil
}

func (m Model) handleSetupMPTKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.setupStep = setupWelcome
		return m, nil
	case "ctrl+s":
		return m.submitSetupMPT()
	case "tab", "down":
		if m.setupFocusIdx < 2 {
			m.setupFocusIdx++
			m.syncSetupMPTFocus()
		}
		return m, textinput.Blink
	case "shift+tab", "up":
		if m.setupFocusIdx > 0 {
			m.setupFocusIdx--
			m.syncSetupMPTFocus()
		}
		return m, textinput.Blink
	case "enter":
		if m.setupFocusIdx < 2 {
			m.setupFocusIdx++
			m.syncSetupMPTFocus()
			return m, textinput.Blink
		}
		return m.submitSetupMPT()
	}
	var cmd tea.Cmd
	m.setupMPTInputs[m.setupFocusIdx], cmd = m.setupMPTInputs[m.setupFocusIdx].Update(msg)
	return m, cmd
}

func (m Model) submitSetupMPT() (tea.Model, tea.Cmd) {
	url := strings.TrimSpace(m.setupMPTInputs[0].Value())
	if url == "" {
		m.setupErr = "URL da API é obrigatória"
		return m, nil
	}
	m.cfg.MPT.BaseURL = url
	m.cfg.MPT.VoiceName = strings.TrimSpace(m.setupMPTInputs[1].Value())
	m.cfg.MPT.VideoLanguage = strings.TrimSpace(m.setupMPTInputs[2].Value())
	m.setupErr = ""

	if m.setupChoice == choiceFull {
		m.setupStep = setupUploadPost
		m.setupFocusIdx = 0
		m.syncSetupUPFocus()
		return m, textinput.Blink
	}
	// MPT-only: save and finish
	m.cfg.Save(m.cfgPath)
	m.setupStep = setupDone
	return m, nil
}

func (m Model) handleSetupUPKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.setupStep = setupMPT
		m.setupFocusIdx = 0
		m.syncSetupMPTFocus()
		return m, textinput.Blink
	case "ctrl+s":
		return m.submitSetupUP()
	case "tab", "down":
		if m.setupFocusIdx < 1 {
			m.setupFocusIdx++
			m.syncSetupUPFocus()
		}
		return m, textinput.Blink
	case "shift+tab", "up":
		if m.setupFocusIdx > 0 {
			m.setupFocusIdx--
			m.syncSetupUPFocus()
		}
		return m, textinput.Blink
	case "enter":
		if m.setupFocusIdx < 1 {
			m.setupFocusIdx++
			m.syncSetupUPFocus()
			return m, textinput.Blink
		}
		return m.submitSetupUP()
	}
	var cmd tea.Cmd
	m.setupUPInputs[m.setupFocusIdx], cmd = m.setupUPInputs[m.setupFocusIdx].Update(msg)
	return m, cmd
}

func (m Model) submitSetupUP() (tea.Model, tea.Cmd) {
	key := strings.TrimSpace(m.setupUPInputs[0].Value())
	user := strings.TrimSpace(m.setupUPInputs[1].Value())
	if key == "" || user == "" {
		m.setupErr = "API Key e Username são obrigatórios"
		return m, nil
	}
	m.cfg.UploadPost.APIKey = key
	m.cfg.UploadPost.Username = user
	m.cfg.UploadPost.PrivacyLevel = "PUBLIC_TO_EVERYONE"
	m.setupErr = ""
	m.cfg.Save(m.cfgPath)
	m.setupStep = setupDone
	return m, nil
}

func (m Model) handleSetupDoneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter", " ":
		m.view = viewDashboard
		m.refresh()
		return m, nil
	}
	return m, nil
}

func (m *Model) syncSetupMPTFocus() {
	for i := range m.setupMPTInputs {
		if i == m.setupFocusIdx {
			m.setupMPTInputs[i].Focus()
		} else {
			m.setupMPTInputs[i].Blur()
		}
	}
}

func (m *Model) syncSetupUPFocus() {
	for i := range m.setupUPInputs {
		if i == m.setupFocusIdx {
			m.setupUPInputs[i].Focus()
		} else {
			m.setupUPInputs[i].Blur()
		}
	}
}

// ── Dashboard keys ────────────────────────────────────────────────────────────

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

// ── Add-channel form keys ─────────────────────────────────────────────────────

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
		m.inPlatforms = true
		m.platFocus = 0
		m.inputs[m.focusIdx].Blur()
		return m, nil
	}

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
		m.formErr = "Nome do canal e nicho são obrigatórios"
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
		m.formErr = "Selecione pelo menos uma plataforma"
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
		m.formErr = fmt.Sprintf("Erro ao salvar: %v", err)
		return m, nil
	}
	if m.manager != nil {
		m.manager.Start(ch)
	}

	m.addLog(fmt.Sprintf("✨ Canal '%s' criado (%s, %d/dia)", name, niche, count))
	m.view = viewDashboard
	m.formErr = ""
	m.refresh()
	return m, nil
}

// ── Data helpers ──────────────────────────────────────────────────────────────

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
	m.logs = append(m.logs, fmt.Sprintf("[%s] %s", ts, line))
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

// ── Render: Setup Wizard ──────────────────────────────────────────────────────

func (m Model) renderSetup() string {
	switch m.setupStep {
	case setupWelcome:
		return m.renderSetupWelcome()
	case setupMPT:
		return m.renderSetupMPT()
	case setupUploadPost:
		return m.renderSetupUP()
	case setupDone:
		return m.renderSetupDone()
	}
	return ""
}

func (m Model) renderSetupWelcome() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("◆ NicheBot — Configuração Inicial"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  Olá! Vamos configurar o NicheBot antes de começar."))
	b.WriteString("\n  ")
	b.WriteString(dimStyle.Render("O que você quer configurar?"))
	b.WriteString("\n\n")

	type option struct {
		label    string
		subtitle string
	}
	options := []option{
		{
			label:    "Configuração Completa  (recomendado)",
			subtitle: "MoneyPrinterTurbo + Upload-Post → gera e posta automaticamente",
		},
		{
			label:    "Só o MoneyPrinterTurbo",
			subtitle: "Configura apenas a geração de vídeos (posting manual depois)",
		},
		{
			label:    "Pular por agora",
			subtitle: "Abre o dashboard com valores padrão (edite nichebot.toml manualmente)",
		},
	}

	for i, opt := range options {
		if i == m.setupChoice {
			line := fmt.Sprintf("  ▸ %s\n    %s", opt.label, opt.subtitle)
			b.WriteString(selectedMenuStyle.Render(line))
		} else {
			line := fmt.Sprintf("    %s\n    %s", opt.label, opt.subtitle)
			b.WriteString(menuStyle.Render(line))
		}
		b.WriteString("\n\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  [↑↓] Navegar   [Enter] Selecionar   [q] Sair"))

	return docStyle.Render(b.String())
}

func (m Model) renderSetupMPT() string {
	var b strings.Builder

	stepLabel := "Passo 1 de 2"
	if m.setupChoice == choiceMPTOnly {
		stepLabel = "Configuração"
	}

	b.WriteString(titleStyle.Render(fmt.Sprintf("◆ %s — MoneyPrinterTurbo API", stepLabel)))
	b.WriteString("\n\n")

	info := "  O MoneyPrinterTurbo gera os vídeos automaticamente.\n" +
		"  Ele usa um LLM para criar o roteiro, busca clipes no Pexels/Pixabay\n" +
		"  e monta o vídeo com narração e legendas.\n\n" +
		"  Precisa estar rodando ANTES de usar o NicheBot.\n\n" +
		"  💻 Na mesma máquina?  →  http://localhost:8080\n" +
		"  🖥  Em uma VPS remota? →  http://SEU_IP_VPS:8080"
	b.WriteString(infoBox.Render(info))
	b.WriteString("\n\n")

	labels := []string{
		"URL da API  (onde o MPT está rodando)",
		"Voz da narração  (Edge TTS — lista completa no README do MPT)",
		"Idioma dos vídeos  (en, pt, es... ou vazio para auto-detectar)",
	}
	for i, inp := range m.setupMPTInputs {
		focused := i == m.setupFocusIdx
		if focused {
			b.WriteString(activeStyle.Render("  ▸ " + labels[i]))
		} else {
			b.WriteString(dimStyle.Render("    " + labels[i]))
		}
		b.WriteString("\n")
		if focused {
			b.WriteString("    " + focusedBorder.Render(inp.View()))
		} else {
			b.WriteString("    " + blurredBorder.Render(inp.View()))
		}
		b.WriteString("\n\n")
	}

	if m.setupErr != "" {
		b.WriteString(errStyle.Render("  ⚠ " + m.setupErr))
		b.WriteString("\n\n")
	}

	b.WriteString(helpStyle.Render("  [Tab] Próximo   [Ctrl+S] Continuar   [Esc] Voltar"))
	return docStyle.Render(b.String())
}

func (m Model) renderSetupUP() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("◆ Passo 2 de 2 — Upload-Post"))
	b.WriteString("\n\n")

	info := "  O Upload-Post envia seus vídeos para TikTok, Instagram e\n" +
		"  Facebook com uma única chamada de API — sem precisar logar\n" +
		"  em cada plataforma manualmente.\n\n" +
		"  Como obter suas chaves:\n" +
		"  1. Acesse  upload-post.com\n" +
		"  2. Crie sua conta  (~$29/mês, suporte a TT + IG + FB + YT)\n" +
		"  3. Vá em  Dashboard → API Keys\n" +
		"  4. Copie a  API Key  e o  Username  para os campos abaixo"
	b.WriteString(infoBox.Render(info))
	b.WriteString("\n\n")

	labels := []string{
		"API Key  (Dashboard → API Keys → Key)",
		"Username  (seu nome de usuário no upload-post.com)",
	}
	for i, inp := range m.setupUPInputs {
		focused := i == m.setupFocusIdx
		if focused {
			b.WriteString(activeStyle.Render("  ▸ " + labels[i]))
		} else {
			b.WriteString(dimStyle.Render("    " + labels[i]))
		}
		b.WriteString("\n")
		if focused {
			b.WriteString("    " + focusedBorder.Render(inp.View()))
		} else {
			b.WriteString("    " + blurredBorder.Render(inp.View()))
		}
		b.WriteString("\n\n")
	}

	b.WriteString(dimStyle.Render("    A API Key é salva localmente em nichebot.toml."))
	b.WriteString("\n\n")

	if m.setupErr != "" {
		b.WriteString(errStyle.Render("  ⚠ " + m.setupErr))
		b.WriteString("\n\n")
	}

	b.WriteString(helpStyle.Render("  [Tab] Próximo   [Ctrl+S] Salvar e Finalizar   [Esc] Voltar"))
	return docStyle.Render(b.String())
}

func (m Model) renderSetupDone() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("◆ Tudo pronto!"))
	b.WriteString("\n\n")
	b.WriteString(okStyle.Render("  ✅  nichebot.toml salvo com sucesso."))
	b.WriteString("\n\n")

	next := "  Agora adicione seu primeiro canal no dashboard:\n\n" +
		"  [a]  Adicionar canal\n" +
		"       → Dê um nome ao canal  (ex: CatLover)\n" +
		"       → Defina o nicho       (ex: funny cat videos)\n" +
		"       → Escolha quantos vídeos por dia  (ex: 3)\n" +
		"       → Selecione as plataformas: TikTok, Instagram, Facebook\n\n" +
		"  O NicheBot vai gerar e postar automaticamente todo dia,\n" +
		"  distribuindo os vídeos das 08:00 às 20:00."
	b.WriteString(infoBox.Render(next))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("  [Enter] Abrir o Dashboard"))
	return docStyle.Render(b.String())
}

// ── Render: Dashboard ─────────────────────────────────────────────────────────

func (m Model) renderDashboard() string {
	var b strings.Builder

	title := titleStyle.Render("◆ NicheBot")
	help := helpStyle.Render("[a] Add  [p] Pause/Resume  [D] Delete  [↑↓] Navegar  [q] Sair")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", help))
	b.WriteString("\n\n")

	b.WriteString(headerStyle.Render("CANAIS"))
	b.WriteString("\n")
	if len(m.channels) == 0 {
		b.WriteString(dimStyle.Render("  Nenhum canal ainda. Pressione [a] para criar um."))
		b.WriteString("\n")
	} else {
		b.WriteString(tableStyle.Render(m.table.View()))
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  ● ativo  ○ pausado   Today = posts feitos hoje / meta diária"))
	b.WriteString("\n\n")

	b.WriteString(headerStyle.Render("ATIVIDADE"))
	b.WriteString("\n")
	if len(m.logs) == 0 {
		b.WriteString(dimStyle.Render("  Aguardando workers…"))
		b.WriteString("\n")
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
	default:
		return "  " + logStyle.Render(line)
	}
}

// ── Render: Add Channel ───────────────────────────────────────────────────────

func (m Model) renderAddChannel() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("◆ Adicionar Canal"))
	b.WriteString("\n\n")

	labels := []string{"Nome do Canal", "Nicho / Tópico", "Vídeos por dia"}
	for i, inp := range m.inputs {
		focused := i == m.focusIdx && !m.inPlatforms
		if focused {
			b.WriteString(activeStyle.Render("  ▸ " + labels[i]))
		} else {
			b.WriteString(dimStyle.Render("    " + labels[i]))
		}
		b.WriteString("\n")
		if focused {
			b.WriteString("    " + focusedBorder.Render(inp.View()))
		} else {
			b.WriteString("    " + blurredBorder.Render(inp.View()))
		}
		b.WriteString("\n\n")
	}

	b.WriteString("\n")
	if m.inPlatforms {
		b.WriteString(activeStyle.Render("  ▸ Plataformas"))
	} else {
		b.WriteString(dimStyle.Render("    Plataformas"))
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
	b.WriteString(helpStyle.Render("  [Tab/↓] Próximo  [Shift+Tab/↑] Anterior  [Space] Toggle  [Ctrl+S] Criar  [Esc] Cancelar"))
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
