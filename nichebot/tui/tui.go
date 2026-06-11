package tui

import (
	"fmt"
	"nichebot/db"
	"nichebot/models"
	"nichebot/worker"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// ── i18n ──────────────────────────────────────────────────────────────────────

var translations = map[string]map[string]string{
	"channels":       {"pt": "CANAIS", "en": "CHANNELS"},
	"activity":       {"pt": "ATIVIDADE", "en": "ACTIVITY"},
	"no_channels":    {"pt": "Nenhum canal ainda. Pressione [a] para criar um.", "en": "No channels yet. Press [a] to add one."},
	"waiting":        {"pt": "Aguardando workers…", "en": "Waiting for workers…"},
	"legend":         {"pt": "● ativo  ○ pausado   Today = posts feitos hoje / meta", "en": "● active  ○ paused   Today = posts done today / target"},
	"help_dash":      {"pt": "[a] Add  [Enter] Detalhes  [p] Pause  [D] Delete  [s] Config  [q] Sair", "en": "[a] Add  [Enter] Detail  [p] Pause  [D] Delete  [s] Settings  [q] Quit"},
	"pending_header": {"pt": "AGUARDANDO APROVAÇÃO", "en": "PENDING APPROVAL"},
	"history_header": {"pt": "HISTÓRICO", "en": "HISTORY"},
	"no_pending":     {"pt": "Nenhum vídeo aguardando aprovação.", "en": "No videos pending approval."},
	"no_history":     {"pt": "Nenhum post ainda.", "en": "No posts yet."},
	"help_detail":    {"pt": "[A] Aprovar  [R] Rejeitar  [O] Abrir vídeo  [↑↓] Navegar  [Esc] Voltar", "en": "[A] Approve  [R] Reject  [O] Open video  [↑↓] Navigate  [Esc] Back"},
	"config_header":  {"pt": "CONFIGURAÇÃO", "en": "CONFIGURATION"},
	"settings_title": {"pt": "◆ Configurações", "en": "◆ Settings"},
	"lang_label":     {"pt": "Idioma da TUI", "en": "TUI Language"},
	"tg_header":      {"pt": "Telegram — Notificações", "en": "Telegram — Notifications"},
	"tg_hint": {"pt": "Como obter:\n  1. Abra @BotFather no Telegram → /newbot\n  2. Copie o token gerado\n  3. Envie uma msg ao bot e acesse:\n     https://api.telegram.org/bot{TOKEN}/getUpdates\n     O chat_id está no campo \"id\" do objeto \"chat\"",
		"en": "How to get:\n  1. Open @BotFather on Telegram → /newbot\n  2. Copy the generated token\n  3. Send a message to the bot and open:\n     https://api.telegram.org/bot{TOKEN}/getUpdates\n     The chat_id is in the \"id\" field of the \"chat\" object"},
	"save_ok":      {"pt": "✅ Configurações salvas.", "en": "✅ Settings saved."},
	"help_sett":    {"pt": "[Tab] Próximo  [Ctrl+S] Salvar  [Esc] Voltar", "en": "[Tab] Next  [Ctrl+S] Save  [Esc] Back"},
	"add_title":    {"pt": "◆ Adicionar Canal", "en": "◆ Add Channel"},
	"help_form":    {"pt": "[Tab/↓] Próximo  [Shift+Tab/↑] Anterior  [Space] Toggle  [Ctrl+S] Criar  [Esc] Cancelar", "en": "[Tab/↓] Next  [Shift+Tab/↑] Prev  [Space] Toggle  [Ctrl+S] Create  [Esc] Cancel"},
}

func tr(lang, key string) string {
	if m, ok := translations[key]; ok {
		if s, ok := m[lang]; ok {
			return s
		}
		if s, ok := m["pt"]; ok {
			return s
		}
	}
	return key
}

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
	cyan   = lipgloss.Color("#00C8FF")

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
	infoStyle   = lipgloss.NewStyle().Foreground(cyan)
	docStyle    = lipgloss.NewStyle().Margin(1, 2)

	focusedBorder = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(purple)
	blurredBorder = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(dark)
	tableStyle    = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(dark)

	infoBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dark).
		Padding(0, 2).
		Foreground(lipgloss.Color("#AAAAAA"))

	selectedMenuStyle = lipgloss.NewStyle().Bold(true).Foreground(white).Background(purple).Padding(0, 1)
	menuStyle         = lipgloss.NewStyle().Foreground(muted).Padding(0, 1)
)

// ── View states ───────────────────────────────────────────────────────────────

type viewState int

const (
	viewSetup         viewState = iota
	viewDashboard
	viewChannelDetail
	viewAddChannel
	viewSettings
)

// setup sub-steps
const (
	setupWelcome    = 0
	setupMPT        = 1
	setupUploadPost = 2
	setupDone       = 3
)

const (
	choiceFull    = 0
	choiceMPTOnly = 1
	choiceSkip    = 2
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
	lang    string // "pt" or "en"

	// Dashboard
	channels []models.Channel
	posts    []models.Post
	table    table.Model
	logs     []string

	// Channel detail
	detailCh      models.Channel
	detailPending []models.Post
	detailHistory []models.Post
	pendingTable  table.Model
	historyTable  table.Model
	detailInHist  bool // focus is on history table (vs pending)

	// Add-channel form  (5 text inputs + platforms)
	inputs      [6]textinput.Model // name, niche, count, cta, affLink, videoLang
	focusIdx    int
	platforms   map[models.Platform]bool
	platList    []models.Platform
	platFocus   int
	inPlatforms bool
	formErr     string

	// Settings
	settInputs   [2]textinput.Model // telegram: bot_token, chat_id
	settFocusIdx int
	settLangSel  int    // 0=PT, 1=EN
	settMsg      string // feedback after save

	// Setup wizard
	setupStep      int
	setupChoice    int
	setupMPTInputs [3]textinput.Model
	setupUPInputs  [2]textinput.Model
	setupFocusIdx  int
	setupErr       string

	width  int
	height int
}

func New(database *db.DB, mgr *worker.Manager, cfg *models.Config, cfgPath string, firstRun bool) Model {
	lang := cfg.UILanguage
	if lang == "" {
		lang = "pt"
	}

	startView := viewDashboard
	if firstRun {
		startView = viewSetup
	}

	// ── Add-channel form inputs ────────────────────────────────────────────────
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

	cta := textinput.New()
	cta.Placeholder = "Produtos dos meus gatos 🐾 Link na bio!"
	cta.CharLimit = 200
	cta.Width = 44

	affLink := textinput.New()
	affLink.Placeholder = "https://amzn.to/xyz  (opcional)"
	affLink.CharLimit = 300
	affLink.Width = 44

	videoLang := textinput.New()
	videoLang.Placeholder = "en  (vazio = usa config global)"
	videoLang.CharLimit = 10
	videoLang.Width = 20

	// ── Settings inputs ────────────────────────────────────────────────────────
	tgToken := textinput.New()
	tgToken.Placeholder = "123456789:AABBccDDeeff..."
	tgToken.Width = 44
	tgToken.CharLimit = 200
	tgToken.EchoMode = textinput.EchoPassword
	tgToken.SetValue(cfg.Telegram.BotToken)
	tgToken.Focus()

	tgChat := textinput.New()
	tgChat.Placeholder = "-1001234567890"
	tgChat.Width = 24
	tgChat.CharLimit = 50
	tgChat.SetValue(cfg.Telegram.ChatID)

	// ── Setup wizard inputs ────────────────────────────────────────────────────
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

	// ── Dashboard table ────────────────────────────────────────────────────────
	t := buildChannelTable()

	// ── Pending table ──────────────────────────────────────────────────────────
	pendCols := []table.Column{
		{Title: "Criado", Width: 8},
		{Title: "Arquivo", Width: 36},
		{Title: "Tamanho", Width: 9},
	}
	pt := table.New(table.WithColumns(pendCols), table.WithFocused(true), table.WithHeight(5))
	applyTableStyle(&pt)

	// ── History table ──────────────────────────────────────────────────────────
	histCols := []table.Column{
		{Title: "Hora", Width: 8},
		{Title: "Status", Width: 9},
		{Title: "Plataformas", Width: 16},
		{Title: "Erro", Width: 28},
	}
	ht := table.New(table.WithColumns(histCols), table.WithFocused(false), table.WithHeight(6))
	applyTableStyle(&ht)

	langSel := 0
	if lang == "en" {
		langSel = 1
	}

	m := Model{
		view:    startView,
		db:      database,
		manager: mgr,
		cfg:     cfg,
		cfgPath: cfgPath,
		lang:    lang,
		inputs:  [6]textinput.Model{name, niche, count, cta, affLink, videoLang},
		platforms: map[models.Platform]bool{
			models.PlatformTikTok:    true,
			models.PlatformInstagram: true,
			models.PlatformFacebook:  true,
			models.PlatformYouTube:   false,
		},
		platList: []models.Platform{
			models.PlatformTikTok, models.PlatformInstagram,
			models.PlatformFacebook, models.PlatformYouTube,
		},
		table:          t,
		pendingTable:   pt,
		historyTable:   ht,
		settInputs:     [2]textinput.Model{tgToken, tgChat},
		settLangSel:    langSel,
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

func buildChannelTable() table.Model {
	cols := []table.Column{
		{Title: "Canal", Width: 14},
		{Title: "Nicho", Width: 20},
		{Title: "Plataformas", Width: 11},
		{Title: "Today", Width: 7},
		{Title: "Total", Width: 7},
		{Title: "Falhas", Width: 7},
		{Title: "Próximo", Width: 12},
		{Title: "•", Width: 2},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(8))
	applyTableStyle(&t)
	return t
}

func applyTableStyle(t *table.Model) {
	ts := table.DefaultStyles()
	ts.Header = ts.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(dark).
		BorderBottom(true).
		Bold(true)
	ts.Selected = ts.Selected.Foreground(white).Background(purple).Bold(true)
	t.SetStyles(ts)
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
		} else if m.view == viewChannelDetail {
			m.refreshDetail(m.detailCh.ID)
		}
		return m, tickEvery(30 * time.Second)

	case worker.PostUpdateMsg:
		m.addLog(msg.LogLine)
		if m.view == viewDashboard {
			m.refresh()
		} else if m.view == viewChannelDetail {
			m.refreshDetail(m.detailCh.ID)
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
	case viewChannelDetail:
		return m.renderChannelDetail()
	case viewAddChannel:
		return m.renderAddChannel()
	case viewSettings:
		return m.renderSettings()
	}
	return ""
}

// ── Key routing ───────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewSetup:
		return m.handleSetupKey(msg)
	case viewChannelDetail:
		return m.handleDetailKey(msg)
	case viewAddChannel:
		return m.handleFormKey(msg)
	case viewSettings:
		return m.handleSettingsKey(msg)
	default:
		return m.handleDashKey(msg)
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

	case "s":
		m.view = viewSettings
		m.settMsg = ""
		m.settFocusIdx = 0
		m.settInputs[0].Focus()
		m.settInputs[1].Blur()
		return m, textinput.Blink

	case "enter":
		if ch := m.selectedChannel(); ch != nil {
			m.detailCh = *ch
			m.detailInHist = false
			m.refreshDetail(ch.ID)
			m.view = viewChannelDetail
		}
		return m, nil

	case "p":
		if ch := m.selectedChannel(); ch != nil {
			if ch.Status == models.StatusActive {
				m.db.UpdateChannelStatus(ch.ID, models.StatusPaused)
				if m.manager != nil {
					m.manager.Stop(ch.ID)
				}
				m.addLog(fmt.Sprintf("⏸  [%s] pausado", ch.Name))
			} else {
				m.db.UpdateChannelStatus(ch.ID, models.StatusActive)
				if m.manager != nil {
					m.manager.Start(*ch)
				}
				m.addLog(fmt.Sprintf("▶  [%s] retomado", ch.Name))
			}
			m.refresh()
		}

	case "D":
		if ch := m.selectedChannel(); ch != nil {
			if m.manager != nil {
				m.manager.Stop(ch.ID)
			}
			m.db.DeleteChannel(ch.ID)
			m.addLog(fmt.Sprintf("🗑  [%s] deletado", ch.Name))
			m.refresh()
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// ── Channel detail keys ───────────────────────────────────────────────────────

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewDashboard
		m.refresh()
		return m, nil

	case "tab":
		m.detailInHist = !m.detailInHist
		return m, nil

	case "A", "a":
		if !m.detailInHist && len(m.detailPending) > 0 {
			idx := m.pendingTable.Cursor()
			if idx < len(m.detailPending) {
				post := m.detailPending[idx]
				if m.manager != nil {
					m.manager.ApproveAndPost(post.ID)
				}
				m.addLog(fmt.Sprintf("✅ [%s] Aprovado: %s", m.detailCh.Name, truncStr(post.VideoPath, 30)))
				m.refreshDetail(m.detailCh.ID)
			}
		}
		return m, nil

	case "R", "r":
		if !m.detailInHist && len(m.detailPending) > 0 {
			idx := m.pendingTable.Cursor()
			if idx < len(m.detailPending) {
				post := m.detailPending[idx]
				if m.manager != nil {
					m.manager.RejectPost(post.ID)
				}
				m.addLog(fmt.Sprintf("🗑  [%s] Rejeitado", m.detailCh.Name))
				m.refreshDetail(m.detailCh.ID)
			}
		}
		return m, nil

	case "O", "o":
		// Open video with system default player
		if !m.detailInHist && len(m.detailPending) > 0 {
			idx := m.pendingTable.Cursor()
			if idx < len(m.detailPending) {
				openFile(m.detailPending[idx].VideoPath)
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.detailInHist {
		m.historyTable, cmd = m.historyTable.Update(msg)
	} else {
		m.pendingTable, cmd = m.pendingTable.Update(msg)
	}
	return m, cmd
}

// ── Settings keys ─────────────────────────────────────────────────────────────

func (m Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.view = viewDashboard
		return m, nil
	case "ctrl+s":
		return m.saveSettings()
	case "tab", "down":
		if m.settFocusIdx < 1 {
			m.settFocusIdx++
			m.syncSettFocus()
		}
		return m, textinput.Blink
	case "shift+tab", "up":
		if m.settFocusIdx > 0 {
			m.settFocusIdx--
			m.syncSettFocus()
		}
		return m, textinput.Blink
	case "left", "right":
		m.settLangSel = 1 - m.settLangSel
		return m, nil
	}
	var cmd tea.Cmd
	m.settInputs[m.settFocusIdx], cmd = m.settInputs[m.settFocusIdx].Update(msg)
	return m, cmd
}

func (m Model) saveSettings() (tea.Model, tea.Cmd) {
	m.cfg.Telegram.BotToken = strings.TrimSpace(m.settInputs[0].Value())
	m.cfg.Telegram.ChatID = strings.TrimSpace(m.settInputs[1].Value())
	m.cfg.Telegram.Enabled = m.cfg.Telegram.BotToken != "" && m.cfg.Telegram.ChatID != ""
	if m.settLangSel == 0 {
		m.cfg.UILanguage = "pt"
		m.lang = "pt"
	} else {
		m.cfg.UILanguage = "en"
		m.lang = "en"
	}
	m.cfg.Save(m.cfgPath)
	m.settMsg = tr(m.lang, "save_ok")
	return m, nil
}

func (m *Model) syncSettFocus() {
	for i := range m.settInputs {
		if i == m.settFocusIdx {
			m.settInputs[i].Focus()
		} else {
			m.settInputs[i].Blur()
		}
	}
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
		} else if m.focusIdx < 5 {
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
				m.focusIdx = 5
				m.syncFocus()
			}
		} else if m.focusIdx > 0 {
			m.focusIdx--
			m.syncFocus()
		}
		return m, textinput.Blink
	case "enter":
		// Enter only advances field — space is NOT intercepted here (passes to input)
		if m.inPlatforms {
			return m, nil
		}
		if m.focusIdx < 5 {
			m.focusIdx++
			m.syncFocus()
			return m, textinput.Blink
		}
		m.inPlatforms = true
		m.platFocus = 0
		m.inputs[m.focusIdx].Blur()
		return m, nil
	case " ":
		// Space toggles platforms when in platform mode; otherwise passes to text input
		if m.inPlatforms {
			m.platforms[m.platList[m.platFocus]] = !m.platforms[m.platList[m.platFocus]]
			return m, nil
		}
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
	cta := strings.TrimSpace(m.inputs[3].Value())
	affLink := strings.TrimSpace(m.inputs[4].Value())
	videoLang := strings.TrimSpace(m.inputs[5].Value())

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
		ID:            uuid.NewString(),
		Name:          name,
		Niche:         niche,
		Platforms:     chosen,
		VideosPerDay:  count,
		Status:        models.StatusActive,
		CreatedAt:     time.Now(),
		CTAText:       cta,
		AffiliateLink: affLink,
		VideoLanguage: videoLang,
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
		if m.setupChoice == choiceSkip {
			m.cfg.Save(m.cfgPath)
			m.view = viewDashboard
			m.refresh()
			return m, nil
		}
		m.setupStep = setupMPT
		m.setupFocusIdx = 0
		m.syncSetupMPTFocus()
		return m, textinput.Blink
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

// ── Data helpers ──────────────────────────────────────────────────────────────

func (m *Model) refresh() {
	channels, _ := m.db.GetChannels()
	m.channels = channels
	m.posts, _ = m.db.GetRecentPosts(20)
	m.rebuildTable()
}

func (m *Model) refreshDetail(channelID string) {
	m.detailPending, _ = m.db.GetPendingPosts(channelID)
	m.detailHistory, _ = m.db.GetChannelPosts(channelID, 15)

	// Rebuild pending table
	rows := make([]table.Row, len(m.detailPending))
	for i, p := range m.detailPending {
		rows[i] = table.Row{
			p.CreatedAt.Format("15:04"),
			truncStr(p.VideoPath, 36),
			fileSize(p.VideoPath),
		}
	}
	m.pendingTable.SetRows(rows)

	// Rebuild history table
	hrows := make([]table.Row, len(m.detailHistory))
	for i, p := range m.detailHistory {
		hrows[i] = table.Row{
			p.CreatedAt.Format("15:04"),
			postStatusIcon(p.Status),
			platNames(p.PlatformsPosted),
			truncStr(p.Error, 28),
		}
	}
	m.historyTable.SetRows(hrows)
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
			nextPost(ch, stats),
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
	for i := range m.inputs {
		m.inputs[i].SetValue("")
	}
	m.inputs[2].SetValue("3")
	m.focusIdx = 0
	m.syncFocus()
	m.inPlatforms = false
	m.platFocus = 0
	m.platforms = map[models.Platform]bool{
		models.PlatformTikTok: true, models.PlatformInstagram: true,
		models.PlatformFacebook: true, models.PlatformYouTube: false,
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

	title := titleStyle.Render("◆ NicheBot")
	help := helpStyle.Render(tr(m.lang, "help_dash"))
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", help))
	b.WriteString("\n\n")

	b.WriteString(headerStyle.Render(tr(m.lang, "channels")))
	b.WriteString("\n")
	if len(m.channels) == 0 {
		b.WriteString(dimStyle.Render("  " + tr(m.lang, "no_channels")))
		b.WriteString("\n")
	} else {
		b.WriteString(tableStyle.Render(m.table.View()))
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  " + tr(m.lang, "legend")))
	b.WriteString("\n\n")

	b.WriteString(headerStyle.Render(tr(m.lang, "activity")))
	b.WriteString("\n")
	if len(m.logs) == 0 {
		b.WriteString(dimStyle.Render("  " + tr(m.lang, "waiting")))
		b.WriteString("\n")
	} else {
		visible := m.logs
		if len(visible) > 10 {
			visible = visible[len(visible)-10:]
		}
		for _, line := range visible {
			b.WriteString(styledLog(line) + "\n")
		}
	}

	return docStyle.Render(b.String())
}

// ── Render: Channel Detail ────────────────────────────────────────────────────

func (m Model) renderChannelDetail() string {
	var b strings.Builder
	ch := m.detailCh

	// Title
	statusDot := activeStyle.Render("●")
	if ch.Status == models.StatusPaused {
		statusDot = pausedStyle.Render("○")
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf("◆ %s", ch.Name)))
	b.WriteString("  " + statusDot)
	b.WriteString("\n\n")

	// Config summary
	b.WriteString(headerStyle.Render(tr(m.lang, "config_header")))
	b.WriteString("\n")
	cfg := [][]string{
		{"Nicho", ch.Niche},
		{"Plataformas", platIcons(ch.Platforms)},
		{"Vídeos/dia", strconv.Itoa(ch.VideosPerDay)},
		{"Idioma", orDefault(ch.VideoLanguage, m.cfg.MPT.VideoLanguage+" (global)")},
		{"CTA", orDefault(ch.CTAText, dimStyle.Render("(não definido)"))},
		{"Afiliado", orDefault(ch.AffiliateLink, dimStyle.Render("(não definido)"))},
	}
	for _, row := range cfg {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			dimStyle.Render(padRight(row[0]+":", 12)),
			row[1]))
	}
	b.WriteString("\n")

	// Pending approval
	pendFocused := !m.detailInHist
	pendHeader := tr(m.lang, "pending_header")
	if pendFocused {
		b.WriteString(headerStyle.Render("▸ " + pendHeader))
	} else {
		b.WriteString(dimStyle.Render("  " + pendHeader))
	}
	b.WriteString("\n")
	if len(m.detailPending) == 0 {
		b.WriteString(dimStyle.Render("  " + tr(m.lang, "no_pending")))
		b.WriteString("\n")
	} else {
		b.WriteString(tableStyle.Render(m.pendingTable.View()))
	}
	b.WriteString("\n")

	// History
	histFocused := m.detailInHist
	histHeader := tr(m.lang, "history_header")
	if histFocused {
		b.WriteString(headerStyle.Render("▸ " + histHeader))
	} else {
		b.WriteString(dimStyle.Render("  " + histHeader))
	}
	b.WriteString("\n")
	if len(m.detailHistory) == 0 {
		b.WriteString(dimStyle.Render("  " + tr(m.lang, "no_history")))
		b.WriteString("\n")
	} else {
		b.WriteString(tableStyle.Render(m.historyTable.View()))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("  " + tr(m.lang, "help_detail") + "  [Tab] Trocar tabela"))

	return docStyle.Render(b.String())
}

// ── Render: Settings ─────────────────────────────────────────────────────────

func (m Model) renderSettings() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(tr(m.lang, "settings_title")))
	b.WriteString("\n\n")

	// Language toggle
	b.WriteString(dimStyle.Render("  " + tr(m.lang, "lang_label")))
	b.WriteString("\n  ")
	langs := []string{"Português", "English"}
	for i, l := range langs {
		if i == m.settLangSel {
			b.WriteString(selectedMenuStyle.Render(l))
		} else {
			b.WriteString(menuStyle.Render(l))
		}
		b.WriteString("  ")
	}
	b.WriteString("\n  " + dimStyle.Render("[←/→] Selecionar"))
	b.WriteString("\n\n")

	// Telegram
	b.WriteString(headerStyle.Render(tr(m.lang, "tg_header")))
	b.WriteString("\n")
	b.WriteString(infoBox.Render(tr(m.lang, "tg_hint")))
	b.WriteString("\n\n")

	tgLabels := []string{"Bot Token  (oculto)", "Chat ID"}
	for i, inp := range m.settInputs {
		focused := i == m.settFocusIdx
		if focused {
			b.WriteString(activeStyle.Render("  ▸ " + tgLabels[i]))
		} else {
			b.WriteString(dimStyle.Render("    " + tgLabels[i]))
		}
		b.WriteString("\n")
		if focused {
			b.WriteString("    " + focusedBorder.Render(inp.View()))
		} else {
			b.WriteString("    " + blurredBorder.Render(inp.View()))
		}
		b.WriteString("\n\n")
	}

	if m.settMsg != "" {
		b.WriteString(okStyle.Render("  " + m.settMsg))
		b.WriteString("\n\n")
	}

	b.WriteString(helpStyle.Render("  " + tr(m.lang, "help_sett")))
	return docStyle.Render(b.String())
}

// ── Render: Add Channel form ──────────────────────────────────────────────────

func (m Model) renderAddChannel() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(tr(m.lang, "add_title")))
	b.WriteString("\n\n")

	type fieldMeta struct{ label, hint string }
	fields := []fieldMeta{
		{"Nome do Canal", ""},
		{"Nicho / Tópico", ""},
		{"Vídeos por dia", ""},
		{"CTA — call-to-action  (todos os posts)", ""},
		{"Link de afiliado  (Facebook e YouTube apenas)", "⚠  TikTok e Instagram não permitem links clicáveis em posts."},
		{"Idioma do vídeo  (ex: en, pt, es)", "Vazio = usa o idioma global da config."},
	}
	for i, inp := range m.inputs {
		focused := i == m.focusIdx && !m.inPlatforms
		label := fields[i].label
		if focused {
			b.WriteString(activeStyle.Render("  ▸ " + label))
		} else {
			b.WriteString(dimStyle.Render("    " + label))
		}
		b.WriteString("\n")
		if focused {
			b.WriteString("    " + focusedBorder.Render(inp.View()))
		} else {
			b.WriteString("    " + blurredBorder.Render(inp.View()))
		}
		if fields[i].hint != "" {
			b.WriteString("\n    " + dimStyle.Render(fields[i].hint))
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
	b.WriteString(helpStyle.Render("  " + tr(m.lang, "help_form")))
	return docStyle.Render(b.String())
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
	b.WriteString(dimStyle.Render("  Olá! Vamos configurar o NicheBot. O que você quer configurar?"))
	b.WriteString("\n\n")

	type opt struct{ label, sub string }
	opts := []opt{
		{"Configuração Completa  (recomendado)", "MoneyPrinterTurbo + Upload-Post → gera e posta automaticamente"},
		{"Só o MoneyPrinterTurbo", "Configura apenas a geração de vídeos (posting manual depois)"},
		{"Pular por agora", "Abre o dashboard com valores padrão (edite nichebot.toml manualmente)"},
	}
	for i, o := range opts {
		line := fmt.Sprintf("  ▸ %s\n    %s", o.label, o.sub)
		if i == m.setupChoice {
			b.WriteString(selectedMenuStyle.Render(line))
		} else {
			b.WriteString(menuStyle.Render(fmt.Sprintf("    %s\n    %s", o.label, o.sub)))
		}
		b.WriteString("\n\n")
	}
	b.WriteString(helpStyle.Render("  [↑↓] Navegar   [Enter] Selecionar   [q] Sair"))
	return docStyle.Render(b.String())
}

func (m Model) renderSetupMPT() string {
	var b strings.Builder
	step := "Passo 1 de 2"
	if m.setupChoice == choiceMPTOnly {
		step = "Configuração"
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf("◆ %s — MoneyPrinterTurbo API", step)))
	b.WriteString("\n\n")
	b.WriteString(infoBox.Render(
		"  O MPT gera os vídeos automaticamente.\n\n" +
			"  💻 Na mesma máquina?  →  http://localhost:8080\n" +
			"  🖥  Em uma VPS remota? →  http://SEU_IP_VPS:8080"))
	b.WriteString("\n\n")
	labels := []string{
		"URL da API  (onde o MPT está rodando)",
		"Voz da narração  (Edge TTS)",
		"Idioma padrão dos vídeos  (en, pt, es…)",
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
	b.WriteString(infoBox.Render(
		"  O Upload-Post envia o vídeo para TikTok, Instagram e Facebook.\n\n" +
			"  1. Acesse  upload-post.com\n" +
			"  2. Crie sua conta  (~$29/mês)\n" +
			"  3. Vá em  Dashboard → API Keys\n" +
			"  4. Copie a API Key e o Username"))
	b.WriteString("\n\n")
	labels := []string{"API Key", "Username"}
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
	if m.setupErr != "" {
		b.WriteString(errStyle.Render("  ⚠ " + m.setupErr))
		b.WriteString("\n\n")
	}
	b.WriteString(helpStyle.Render("  [Tab] Próximo   [Ctrl+S] Salvar   [Esc] Voltar"))
	return docStyle.Render(b.String())
}

func (m Model) renderSetupDone() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("◆ Tudo pronto!"))
	b.WriteString("\n\n")
	b.WriteString(okStyle.Render("  ✅  nichebot.toml salvo com sucesso."))
	b.WriteString("\n\n")
	b.WriteString(infoBox.Render(
		"  Próximos passos:\n\n" +
			"  [a]  Adicionar canal  → escolha o nicho, plataformas e vídeos por dia\n" +
			"  [s]  Configurações   → Telegram, idioma da TUI\n\n" +
			"  Os vídeos gerados ficam em  AGUARDANDO APROVAÇÃO.\n" +
			"  Entre no canal e pressione [A] para aprovar e postar."))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("  [Enter] Abrir o Dashboard"))
	return docStyle.Render(b.String())
}

// ── Log renderer ──────────────────────────────────────────────────────────────

func styledLog(line string) string {
	switch {
	case strings.Contains(line, "✅"):
		return "  " + okStyle.Render(line)
	case strings.Contains(line, "❌"):
		return "  " + errStyle.Render(line)
	case strings.Contains(line, "⏸"), strings.Contains(line, "⏳"):
		return "  " + warnStyle.Render(line)
	case strings.Contains(line, "📤"):
		return "  " + infoStyle.Render(line)
	default:
		return "  " + logStyle.Render(line)
	}
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

func platNames(platforms []models.Platform) string {
	return platIcons(platforms)
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

func nextPost(ch models.Channel, stats db.ChannelStats) string {
	if ch.Status == models.StatusPaused {
		return pausedStyle.Render("paused")
	}
	if stats.ActiveCount > 0 {
		return warnStyle.Render(fmt.Sprintf("%d queued", stats.ActiveCount))
	}
	if stats.TodayCount >= ch.VideosPerDay {
		return okStyle.Render("quota met")
	}
	return warnStyle.Render("filling…")
}

func postStatusIcon(s models.PostStatus) string {
	switch s {
	case models.PostDone:
		return okStyle.Render("✅ done")
	case models.PostFailed:
		return errStyle.Render("❌ fail")
	case models.PostRejected:
		return dimStyle.Render("✗ reject")
	case models.PostPosting:
		return infoStyle.Render("📤 post")
	default:
		return dimStyle.Render(string(s))
	}
}

func fileSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "—"
	}
	mb := float64(info.Size()) / 1024 / 1024
	return fmt.Sprintf("%.1f MB", mb)
}

func openFile(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	cmd.Start()
}

func truncStr(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
