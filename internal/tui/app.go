package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"p2pbk/internal/app"
	"p2pbk/internal/config"
)

const (
	tabHome = iota
	tabDashboard
	tabFolders
	tabBackups
	tabSettings
)

const (
	folderPaneFolders = iota
	folderPaneBackups
)

const (
	settingsConfig = iota
	settingsService
	settingsAdvanced
)

const (
	homeFocusRoots = iota
	homeFocusBackups
	homeFocusSystem
)

const (
	homePaneRoots = iota
	homePaneDetails
	homePaneBackups
	homePaneActivity
)

const (
	homeDetailOverview = iota
	homeDetailRoot
	homeDetailBackup
	homeDetailSystem
	homeDetailHelp
)

var tabs = []string{"Home", "Dashboard", "Folders", "Backups", "Settings"}

var settingsSections = []string{"Config", "Service", "Advanced"}

type model struct {
	cfg        config.Config
	configPath string
	password   string

	status  app.Status
	repo    app.RepositoryStatus
	maint   app.MaintenanceStatus
	daemon  app.DaemonStatus
	summary app.ConfigSummary

	tab          int
	settingsTab  int
	homeFocus    int
	homeMode     int
	width        int
	height       int
	selected     int
	folderFocus  int
	folderIndex  int
	folderBackup int
	loading      bool
	err          error
	message      string
	messageUntil time.Time
	activity     []string
	op           *operationState
	form         *formState
	picker       *dirPickerState
	pending      *pendingAction
	progress     progress.Model
	spinner      spinner.Model
	homeRoots    list.Model
	homeBackups  list.Model
	homeDetail   viewport.Model
	homeActivity viewport.Model
	homeHelp     help.Model
}

type operationState struct {
	cancel func()
	events <-chan app.OperationEvent
	event  app.OperationEvent
}

type pendingAction struct {
	kind     string
	root     string
	snapshot string
	target   string
}

type field struct {
	label  string
	value  string
	secret bool
}

type formState struct {
	kind   string
	title  string
	fields []field
	active int
}

type statusMsg struct {
	status  app.Status
	repo    app.RepositoryStatus
	maint   app.MaintenanceStatus
	daemon  app.DaemonStatus
	summary app.ConfigSummary
	err     error
}

type actionMsg struct {
	message string
	err     error
}

type opMsg app.OperationEvent

type clearMessageMsg struct{}

type homeKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	NextPane key.Binding
	PrevPane key.Binding
	Details  key.Binding
	Back     key.Binding
	Backup   key.Binding
	Restore  key.Binding
	Add      key.Binding
	Remove   key.Binding
	Password key.Binding
	Audit    key.Binding
	Edit     key.Binding
	Init     key.Binding
	Reset    key.Binding
	Start    key.Binding
	Stop     key.Binding
	Help     key.Binding
	PrevTab  key.Binding
	NextTab  key.Binding
	Quit     key.Binding
	Cancel   key.Binding
}

func (k homeKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.NextPane, k.Details, k.Help, k.Quit}
}

func (k homeKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.NextPane, k.PrevPane, k.Details, k.Back},
		{k.Backup, k.Restore, k.Add, k.Remove, k.Audit},
		{kPasswordBinding(k), k.Edit, k.Init, k.Start, k.Stop, k.Reset},
		{k.PrevTab, k.NextTab, k.Help, k.Quit},
	}
}

func kPasswordBinding(k homeKeyMap) key.Binding {
	return k.Password
}

var homeKeys = homeKeyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "select")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "select")),
	NextPane: key.NewBinding(key.WithKeys("tab", "right", "l"), key.WithHelp("tab", "next pane")),
	PrevPane: key.NewBinding(key.WithKeys("shift+tab", "left", "h"), key.WithHelp("shift+tab", "prev pane")),
	Details:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Backup:   key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "backup")),
	Restore:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restore")),
	Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
	Remove:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove")),
	Password: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "password")),
	Audit:    key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "audit")),
	Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Init:     key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "init")),
	Reset:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "reset")),
	Start:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start")),
	Stop:     key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "stop")),
	Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	PrevTab:  key.NewBinding(key.WithKeys("[", "shift+["), key.WithHelp("[", "prev tab")),
	NextTab:  key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next tab")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Cancel:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel")),
}

type homeListItem struct {
	title string
	desc  string
}

func (i homeListItem) FilterValue() string { return i.title + " " + i.desc }
func (i homeListItem) Title() string       { return i.title }
func (i homeListItem) Description() string { return i.desc }

type homeHelpBindings struct {
	short []key.Binding
	full  [][]key.Binding
}

func (b homeHelpBindings) ShortHelp() []key.Binding {
	return b.short
}

func (b homeHelpBindings) FullHelp() [][]key.Binding {
	return b.full
}

var (
	cyberBg     = lipgloss.Color("#0B0F14")
	cyberPanel  = lipgloss.Color("#3A4756")
	cyberText   = lipgloss.Color("#E6EDF3")
	cyberMuted  = lipgloss.Color("#9AA7B7")
	cyberFocus  = lipgloss.Color("#33D6C5")
	cyberFocus2 = lipgloss.Color("#7CF7EA")
	cyberBlue   = lipgloss.Color("#4AA3FF")
	cyberGreen  = lipgloss.Color("#4ADE80")
	cyberAmber  = lipgloss.Color("#FBBF24")
	cyberRed    = lipgloss.Color("#F87171")
	cyberViolet = lipgloss.Color("#A78BFA")
	cyberRow    = lipgloss.Color("#183041")

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(cyberBg).Background(cyberFocus).Padding(0, 1)
	panelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cyberPanel).Padding(0, 1)
	cardStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cyberPanel).Padding(0, 1)
	cardTitle    = lipgloss.NewStyle().Bold(true).Foreground(cyberFocus)
	cardRule     = lipgloss.NewStyle().Foreground(cyberViolet)
	badgeStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	activeStyle  = lipgloss.NewStyle().Bold(true).Foreground(cyberFocus)
	mutedStyle   = lipgloss.NewStyle().Foreground(cyberMuted)
	warnStyle    = lipgloss.NewStyle().Foreground(cyberAmber)
	errorStyle   = lipgloss.NewStyle().Foreground(cyberRed)
	successStyle = lipgloss.NewStyle().Foreground(cyberGreen)

	homeBorderStyle        = lipgloss.NewStyle().Foreground(cyberPanel)
	homeTitleStyle         = lipgloss.NewStyle().Bold(true).Foreground(cyberFocus)
	homeFocusRoot          = lipgloss.NewStyle().Bold(true).Foreground(cyberBg).Background(cyberFocus2)
	homeFocusBackup        = lipgloss.NewStyle().Bold(true).Foreground(cyberBg).Background(cyberFocus2)
	homeFocusActivityTitle = lipgloss.NewStyle().Bold(true).Foreground(cyberBg).Background(cyberFocus2)
	homeMutedStyle         = lipgloss.NewStyle().Foreground(cyberMuted)
	homeKeyStyle           = lipgloss.NewStyle().Bold(true).Foreground(cyberBg).Background(cyberFocus2).Padding(0, 1)
	homeSelectedRow        = lipgloss.NewStyle().Bold(true).Foreground(cyberFocus2).Background(cyberRow)
	homeOpStyle            = lipgloss.NewStyle().Bold(true).Foreground(cyberBlue)
)

func Run(cfg config.Config, configPath, password string) error {
	tab := tabHome
	if configPath == "" {
		tab = tabSettings
	}
	bar := progress.New(progress.WithGradient("#7CF7EA", "#58B7FF"))
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(cyberFocus2)))
	m := model{cfg: cfg, configPath: configPath, password: password, tab: tab, loading: true, progress: bar, spinner: spin}
	m.initHomeComponents()
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *model) initHomeComponents() {
	m.homeRoots = newHomeList()
	m.homeBackups = newHomeList()
	m.homeDetail = viewport.New(0, 0)
	m.homeActivity = viewport.New(0, 0)
	m.homeHelp = help.New()
	m.homeHelp.ShowAll = false
}

func newHomeList() list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	delegate.Styles.NormalTitle = lipgloss.NewStyle().Foreground(cyberMuted).PaddingLeft(2)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(cyberFocus2).
		Foreground(cyberFocus2).
		Background(cyberRow).
		Bold(true).
		Padding(0, 1)
	l := list.New(nil, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	return l
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchStatus(m.cfg, m.configPath, m.password), tick(), m.spinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
		if m.picker != nil {
			m.picker.resize(size.Width, size.Height)
		}
		m.syncHomeComponents()
		return m, nil
	}
	if m.picker != nil {
		return m.updateDirPicker(msg)
	}
	if m.form != nil {
		return m.updateForm(msg)
	}
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading {
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		if m.tab == tabHome {
			if next, cmd, handled := m.updateHomeKey(msg); handled {
				return next, cmd
			}
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			if m.op != nil {
				m.op.cancel()
			}
			return m, tea.Quit
		case "tab":
			m.tab = (m.tab + 1) % len(tabs)
			m.selected = 0
		case "shift+tab":
			m.tab = (m.tab + len(tabs) - 1) % len(tabs)
			m.selected = 0
		case "right", "l":
			if m.tab == tabFolders && len(m.folderBackups()) > 0 {
				m.folderFocus = folderPaneBackups
			} else if m.tab == tabSettings {
				m.settingsTab = min(m.settingsTab+1, len(settingsSections)-1)
				m.selected = 0
			} else {
				m.tab = (m.tab + 1) % len(tabs)
				m.selected = 0
			}
		case "left", "h":
			if m.tab == tabFolders && m.folderFocus == folderPaneBackups {
				m.folderFocus = folderPaneFolders
			} else if m.tab == tabSettings {
				m.settingsTab = max(0, m.settingsTab-1)
				m.selected = 0
			} else {
				m.tab = (m.tab + len(tabs) - 1) % len(tabs)
				m.selected = 0
			}
		case "[", "shift+[":
			if m.tab == tabSettings {
				m.settingsTab = max(0, m.settingsTab-1)
				m.selected = 0
			}
		case "]":
			if m.tab == tabSettings {
				m.settingsTab = min(m.settingsTab+1, len(settingsSections)-1)
				m.selected = 0
			}
		case "up", "k":
			if m.tab == tabHome {
				m.moveRootSelection(-1)
			} else if m.tab == tabFolders {
				m.moveFolderSelection(-1)
			} else if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.tab == tabHome {
				m.moveRootSelection(1)
			} else if m.tab == tabFolders {
				m.moveFolderSelection(1)
			} else if m.selected < m.maxSelected() {
				m.selected++
			}
		case "c":
			if m.op != nil {
				m.op.cancel()
				m.addActivity("operation cancellation requested")
			}
		case "r":
			if m.tab == tabFolders && m.folderFocus == folderPaneBackups && m.op == nil {
				snapshots := m.folderBackups()
				if len(snapshots) > 0 {
					m.form = restoreForm(snapshots[m.folderBackup].ID)
					return m, nil
				}
			}
			if m.tab == tabBackups && m.op == nil && len(m.status.Snapshots) > 0 {
				m.form = restoreForm(m.status.Snapshots[m.selected].ID)
				return m, nil
			}
			if m.tab == tabHome && m.op == nil {
				if snapshot := m.selectedRootLatestBackup(); snapshot.ID != "" {
					m.form = restoreForm(snapshot.ID)
					return m, nil
				}
			}
			if m.tab == tabDashboard && m.op == nil && len(m.status.Snapshots) > 0 {
				m.form = restoreForm(m.latestBackup().ID)
				return m, nil
			}
			return m, fetchStatus(m.cfg, m.configPath, m.password)
		case "b":
			if (m.tab == tabHome || m.tab == tabDashboard || m.tab == tabFolders) && m.op == nil && len(m.status.Roots) > 0 {
				root := m.status.Roots[m.folderIndex].Path
				if m.password == "" {
					m.pending = &pendingAction{kind: "backup", root: root}
					m.form = passwordForm()
					return m, nil
				}
				return m, m.startBackup(root)
			}
		case "p":
			m.form = passwordForm()
		case "a":
			m.openAddFolder()
		case "e":
			if m.tab == tabSettings && m.settingsTab == settingsConfig {
				m.form = editConfigForm(m.cfg)
			}
		case "i":
			if m.tab == tabSettings && (m.configPath == "" || m.status.Repository == "not initialized") {
				m.form = initForm(m.configPath)
			}
		case "u":
			if m.op == nil {
				return m, auditCmd(m.cfg)
			}
		case "x":
			if m.tab == tabSettings && m.settingsTab == settingsAdvanced {
				m.form = resetForm()
			}
		case "d":
			if m.tab == tabFolders && len(m.status.Roots) > 0 {
				m.form = removeFolderForm(m.status.Roots[m.folderIndex].Path)
			}
		case "s":
			if (m.tab == tabHome || m.tab == tabDashboard || (m.tab == tabSettings && m.settingsTab == settingsService)) && m.op == nil {
				return m, startServiceCmd(m.configPath, m.password)
			}
		case "S":
			if (m.tab == tabHome || m.tab == tabDashboard || (m.tab == tabSettings && m.settingsTab == settingsService)) && m.op == nil {
				return m, stopServiceCmd(m.configPath)
			}
		}
	case statusMsg:
		m.loading = false
		m.status = msg.status
		m.repo = msg.repo
		m.maint = msg.maint
		m.daemon = msg.daemon
		m.summary = msg.summary
		m.err = msg.err
		m.selected = min(m.selected, m.maxSelected())
		m.folderIndex = min(m.folderIndex, max(0, len(m.status.Roots)-1))
		m.folderBackup = min(m.folderBackup, max(0, len(m.folderBackups())-1))
		m.syncHomeComponents()
	case actionMsg:
		if msg.err != nil {
			m.err = msg.err
			m.message = msg.err.Error()
			m.messageUntil = time.Now().Add(2500 * time.Millisecond)
			m.addActivity("error: " + msg.err.Error())
		} else {
			m.err = nil
			m.message = msg.message
			m.messageUntil = time.Now().Add(2500 * time.Millisecond)
			m.addActivity(msg.message)
		}
		return m, tea.Batch(fetchStatus(m.cfg, m.configPath, m.password), clearMessage())
	case configChangedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.message = msg.err.Error()
			m.messageUntil = time.Now().Add(2500 * time.Millisecond)
			m.addActivity("error: " + msg.err.Error())
			return m, clearMessage()
		}
		m.cfg = msg.cfg
		m.configPath = msg.configPath
		m.err = nil
		m.message = msg.message
		m.messageUntil = time.Now().Add(2500 * time.Millisecond)
		m.addActivity(msg.message)
		return m, tea.Batch(fetchStatus(m.cfg, m.configPath, m.password), clearMessage())
	case opMsg:
		event := app.OperationEvent(msg)
		if m.op != nil {
			m.op.event = event
		}
		if event.Done {
			m.op = nil
			if event.Err != nil {
				m.err = event.Err
				m.message = event.Err.Error()
				m.messageUntil = time.Now().Add(2500 * time.Millisecond)
				m.addActivity(event.Kind + " failed: " + event.Err.Error())
			} else {
				m.err = nil
				m.message = event.Message
				m.messageUntil = time.Now().Add(2500 * time.Millisecond)
				m.addActivity(event.Message)
			}
			return m, tea.Batch(fetchStatus(m.cfg, m.configPath, m.password), clearMessage())
		}
		return m, waitOperation(m.op.events)
	case clearMessageMsg:
		if !m.messageUntil.IsZero() && time.Now().After(m.messageUntil) {
			m.message = ""
			m.err = nil
			m.messageUntil = time.Time{}
		}
	case time.Time:
		if !m.messageUntil.IsZero() && time.Now().After(m.messageUntil) {
			m.message = ""
			m.err = nil
			m.messageUntil = time.Time{}
		}
		if m.op == nil {
			return m, tea.Batch(fetchStatus(m.cfg, m.configPath, m.password), tick())
		}
		return m, tick()
	}
	return m, nil
}

func (m model) View() string {
	if m.loading {
		return "\n  " + m.spinner.View() + " " + mutedStyle.Render("syncing p2pbk control plane...") + "\n"
	}
	if m.picker != nil {
		return m.dirPickerView()
	}
	if m.form != nil {
		return m.formView()
	}
	body := strings.Builder{}
	body.WriteString(titleStyle.Render("p2pbk") + " " + mutedStyle.Render("backup cockpit") + "\n")
	body.WriteString(m.tabsView() + "\n\n")
	switch m.tab {
	case tabHome:
		body.WriteString(m.homeView())
	case tabFolders:
		body.WriteString(m.foldersView())
	case tabBackups:
		body.WriteString(m.backupsView())
	case tabSettings:
		body.WriteString(m.settingsView())
	default:
		body.WriteString(m.dashboardView())
	}
	if m.op != nil && m.tab != tabHome && m.tab != tabDashboard {
		body.WriteString("\n\n" + m.operationView())
	}
	body.WriteString("\n\n")
	if m.err != nil {
		body.WriteString(errorStyle.Render(m.friendlyError(m.err)) + "\n")
	} else if notice := m.setupNotice(); notice != "" {
		body.WriteString(warnStyle.Render(notice) + "\n")
	} else if m.message != "" {
		body.WriteString(successStyle.Render(m.message) + "\n")
	}
	if m.tab != tabHome {
		body.WriteString(m.helpView())
	}
	return body.String()
}

func (m model) tabsView() string {
	parts := make([]string, len(tabs))
	for i, name := range tabs {
		if i == m.tab {
			parts[i] = activeStyle.Render("[" + name + "]")
		} else {
			parts[i] = mutedStyle.Render(" " + name + " ")
		}
	}
	return strings.Join(parts, "  ")
}

func (m model) updateHomeKey(msg tea.KeyMsg) (model, tea.Cmd, bool) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.op != nil {
			m.op.cancel()
		}
		return m, tea.Quit, true
	case "esc":
		if m.homeMode != homeDetailOverview {
			m.homeMode = homeDetailOverview
			return m, nil, true
		}
		return m, nil, false
	}
	if m.homeMode != homeDetailOverview {
		if key.Matches(msg, homeKeys.Up) || key.Matches(msg, homeKeys.Down) {
			var cmd tea.Cmd
			m.homeDetail, cmd = m.homeDetail.Update(msg)
			return m, cmd, true
		}
	}
	switch msg.String() {
	case "[", "shift+[":
		m.tab = (m.tab + len(tabs) - 1) % len(tabs)
		m.selected = 0
		m.homeMode = homeDetailOverview
		return m, nil, true
	case "]":
		m.tab = (m.tab + 1) % len(tabs)
		m.selected = 0
		m.homeMode = homeDetailOverview
		return m, nil, true
	case "tab", "right", "l":
		if m.homeMode == homeDetailOverview {
			m.moveHomeFocus(1)
		}
		return m, nil, true
	case "shift+tab", "left", "h":
		if m.homeMode == homeDetailOverview {
			m.moveHomeFocus(-1)
		}
		return m, nil, true
	case "up", "k":
		if m.homeMode == homeDetailOverview {
			return m.updateHomeFocusedComponent(msg)
		}
		return m, nil, true
	case "down", "j":
		if m.homeMode == homeDetailOverview {
			return m.updateHomeFocusedComponent(msg)
		}
		return m, nil, true
	case "enter":
		m.openHomeDetail()
		return m, nil, true
	case "?":
		m.homeMode = homeDetailHelp
		return m, nil, true
	case "c":
		if m.op != nil {
			m.op.cancel()
			m.addActivity("operation cancellation requested")
		}
		return m, nil, true
	case "r":
		if m.op == nil {
			if snapshot := m.selectedHomeSnapshot(); snapshot.ID != "" {
				m.form = restoreForm(snapshot.ID)
				return m, nil, true
			}
		}
		return m, fetchStatus(m.cfg, m.configPath, m.password), true
	case "b":
		if m.op == nil && len(m.status.Roots) > 0 {
			root := m.status.Roots[m.folderIndex].Path
			if m.password == "" {
				m.pending = &pendingAction{kind: "backup", root: root}
				m.form = passwordForm()
				return m, nil, true
			}
			return m, m.startBackup(root), true
		}
		return m, nil, true
	case "p":
		m.form = passwordForm()
		return m, nil, true
	case "a":
		m.openAddFolder()
		return m, nil, true
	case "d":
		if len(m.status.Roots) > 0 {
			m.form = removeFolderForm(m.status.Roots[m.folderIndex].Path)
		}
		return m, nil, true
	case "e":
		m.form = editConfigForm(m.cfg)
		return m, nil, true
	case "i":
		if m.configPath == "" || m.status.Repository == "not initialized" {
			m.form = initForm(m.configPath)
		}
		return m, nil, true
	case "u":
		if m.op == nil {
			return m, auditCmd(m.cfg), true
		}
		return m, nil, true
	case "x":
		if m.homeMode == homeDetailSystem {
			m.form = resetForm()
		}
		return m, nil, true
	case "s":
		if m.op == nil {
			return m, startServiceCmd(m.configPath, m.password), true
		}
		return m, nil, true
	case "S":
		if m.op == nil {
			return m, stopServiceCmd(m.configPath), true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m *model) moveHomeFocus(delta int) {
	next := m.homeFocus + delta
	if next < homeFocusRoots {
		next = homeFocusSystem
	}
	if next > homeFocusSystem {
		next = homeFocusRoots
	}
	m.homeFocus = next
}

func (m model) updateHomeFocusedComponent(msg tea.Msg) (model, tea.Cmd, bool) {
	switch m.homeFocus {
	case homeFocusBackups:
		next, cmd := m.homeBackups.Update(msg)
		m.homeBackups = next
		m.folderBackup = min(m.homeBackups.Index(), max(0, len(m.folderBackups())-1))
		m.homeDetail.SetContent(strings.Join(m.homeOverviewDetailRows(m.homeDetail.Width), "\n"))
		return m, cmd, true
	case homeFocusSystem:
		next, cmd := m.homeActivity.Update(msg)
		m.homeActivity = next
		return m, cmd, true
	default:
		previous := m.folderIndex
		next, cmd := m.homeRoots.Update(msg)
		m.homeRoots = next
		m.folderIndex = min(m.homeRoots.Index(), max(0, len(m.status.Roots)-1))
		if m.folderIndex != previous {
			m.folderBackup = 0
			m.syncHomeComponents()
		} else {
			m.homeDetail.SetContent(strings.Join(m.homeOverviewDetailRows(m.homeDetail.Width), "\n"))
		}
		return m, cmd, true
	}
}

func (m *model) openHomeDetail() {
	switch m.homeFocus {
	case homeFocusBackups:
		if m.selectedHomeSnapshot().ID != "" {
			m.homeMode = homeDetailBackup
		}
	case homeFocusSystem:
		m.homeMode = homeDetailSystem
	default:
		if len(m.status.Roots) > 0 {
			m.homeMode = homeDetailRoot
		}
	}
}

func (m *model) syncHomeComponents() {
	width := m.contentWidth()
	if width < 72 {
		width = 72
	}
	leftWidth, rightWidth, topHeight, bottomHeight := m.homeLayout(width)
	if width < 92 {
		innerWidth := width - 2
		leftWidth = innerWidth
		rightWidth = innerWidth
		topHeight = 6
		bottomHeight = 6
	}
	m.homeRoots.SetSize(max(1, leftWidth-2), max(1, topHeight-1))
	m.homeBackups.SetSize(max(1, leftWidth-2), max(1, bottomHeight-1))
	m.homeDetail.Width = max(1, rightWidth-2)
	m.homeDetail.Height = max(1, topHeight-1)
	m.homeActivity.Width = max(1, rightWidth-2)
	m.homeActivity.Height = max(1, bottomHeight-1)
	m.homeHelp.Width = width

	_ = m.homeRoots.SetItems(m.homeRootItems())
	_ = m.homeBackups.SetItems(m.homeBackupItems())
	m.homeRoots.Select(min(m.folderIndex, max(0, len(m.status.Roots)-1)))
	m.homeBackups.Select(min(m.folderBackup, max(0, len(m.folderBackups())-1)))
	m.homeDetail.SetContent(strings.Join(m.homeOverviewDetailRows(m.homeDetail.Width), "\n"))
	m.homeActivity.SetContent(strings.Join(m.homeActivityRows(m.homeActivity.Width), "\n"))
}

func (m model) homeLayout(width int) (leftWidth, rightWidth, topHeight, bottomHeight int) {
	frameHeight := m.homeFrameHeight()
	bodyHeight := frameHeight - 2
	innerWidth := width - 2
	leftWidth = max(28, min(38, innerWidth/3))
	rightWidth = innerWidth - leftWidth - 1
	topHeight = max(6, (bodyHeight-1)/2)
	bottomHeight = bodyHeight - topHeight - 1
	return leftWidth, rightWidth, topHeight, bottomHeight
}

func (m model) homeRootItems() []list.Item {
	items := make([]list.Item, 0, len(m.status.Roots))
	for _, root := range m.status.Roots {
		items = append(items, homeListItem{title: fmt.Sprintf("%s  %s", homeShortPath(root.Path), root.Protection)})
	}
	if len(items) == 0 {
		items = append(items, homeListItem{title: "No protected folders yet"})
	}
	return items
}

func (m model) homeBackupItems() []list.Item {
	snapshots := m.folderBackups()
	items := make([]list.Item, 0, len(snapshots))
	for i, snapshot := range snapshots {
		label := shortID(snapshot.ID)
		if i == 0 {
			label = "latest"
		}
		items = append(items, homeListItem{title: fmt.Sprintf("%-8s  %-12s  %s", label, compactSnapshotTime(snapshot.StartTime), formatBytes(snapshot.SizeBytes))})
	}
	if len(items) == 0 {
		items = append(items, homeListItem{title: "No snapshots for selected root"})
	}
	return items
}

func (m model) homeView() string {
	width := m.contentWidth()
	if width < 72 {
		width = 72
	}
	m.syncHomeComponents()
	if m.homeMode != homeDetailOverview {
		return m.homeDetailFrame(width) + "\n" + m.homeFooter(width)
	}
	if width < 92 {
		return m.homeStackedFrame(width) + "\n" + m.homeFooter(width)
	}
	return m.homeFrame(width) + "\n" + m.homeFooter(width)
}

func (m model) homeFrame(width int) string {
	frameHeight := m.homeFrameHeight()
	bodyHeight := frameHeight - 2
	innerWidth := width - 2
	leftWidth := max(28, min(38, innerWidth/3))
	rightWidth := innerWidth - leftWidth - 1
	topHeight := max(6, (bodyHeight-1)/2)
	bottomHeight := bodyHeight - topHeight - 1

	roots := m.homePaneView(homePaneRoots, "Roots", m.homeRoots.View(), leftWidth, topHeight, m.homeFocus == homeFocusRoots)
	detail := m.homePaneView(homePaneDetails, m.homeDetailsTitle(), m.homeDetail.View(), rightWidth, topHeight, false)
	snapshots := m.homePaneView(homePaneBackups, "Backups", m.homeBackups.View(), leftWidth, bottomHeight, m.homeFocus == homeFocusBackups)
	activity := m.homePaneView(homePaneActivity, "Activity / System", m.homeActivity.View(), rightWidth, bottomHeight, m.homeFocus == homeFocusSystem)

	lines := []string{m.homeHeader(width)}
	for i := 0; i < topHeight; i++ {
		lines = append(lines, m.homeFrameRow(roots[i], detail[i]))
	}
	lines = append(lines, m.homeDivider(leftWidth, rightWidth))
	for i := 0; i < bottomHeight; i++ {
		lines = append(lines, m.homeFrameRow(snapshots[i], activity[i]))
	}
	lines = append(lines, homeBorderStyle.Render("└"+strings.Repeat("─", leftWidth)+"┴"+strings.Repeat("─", rightWidth)+"┘"))
	return strings.Join(lines, "\n")
}

func (m model) homeStackedFrame(width int) string {
	innerWidth := width - 2
	paneHeight := 6
	sections := [][]string{
		m.homePaneView(homePaneRoots, "Roots", m.homeRoots.View(), innerWidth, paneHeight, m.homeFocus == homeFocusRoots),
		m.homePaneView(homePaneDetails, m.homeDetailsTitle(), m.homeDetail.View(), innerWidth, paneHeight, false),
		m.homePaneView(homePaneBackups, "Backups", m.homeBackups.View(), innerWidth, paneHeight, m.homeFocus == homeFocusBackups),
		m.homePaneView(homePaneActivity, "Activity / System", m.homeActivity.View(), innerWidth, paneHeight, m.homeFocus == homeFocusSystem),
	}
	lines := []string{m.homeHeader(width)}
	for sectionIndex, section := range sections {
		if sectionIndex > 0 {
			lines = append(lines, homeBorderStyle.Render("├"+strings.Repeat("─", innerWidth)+"┤"))
		}
		for _, line := range section {
			lines = append(lines, homeBorderStyle.Render("│")+line+homeBorderStyle.Render("│"))
		}
	}
	lines = append(lines, homeBorderStyle.Render("└"+strings.Repeat("─", innerWidth)+"┘"))
	return strings.Join(lines, "\n")
}

func (m model) homeHeader(width int) string {
	innerWidth := width - 2
	left := " p2pbk "
	right := " " + strings.Join([]string{
		"daemon: " + m.homeDaemonLabel(),
		"peers: " + m.homePeerCountLabel(),
		"repo: " + m.homeRepoLabel(),
		"op: " + m.homeOperationLabel(),
	}, "  ") + " "
	space := innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		availableRight := max(0, innerWidth-lipgloss.Width(left)-1)
		right = " " + middleTruncate(strings.TrimSpace(right), max(0, availableRight-2)) + " "
		space = innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	}
	return homeBorderStyle.Render("┌") + homeTitleStyle.Render(left) + homeBorderStyle.Render(strings.Repeat("─", max(0, space))) + homeMutedStyle.Render(right) + homeBorderStyle.Render("┐")
}

func (m model) homeFrameRow(left, right string) string {
	return homeBorderStyle.Render("│") + left + homeBorderStyle.Render("│") + right + homeBorderStyle.Render("│")
}

func (m model) homeDivider(leftWidth, rightWidth int) string {
	return homeBorderStyle.Render("├" + strings.Repeat("─", leftWidth) + "┼" + strings.Repeat("─", rightWidth) + "┤")
}

func (m model) homePaneView(pane int, title, body string, width, height int, focused bool) []string {
	rows := strings.Split(body, "\n")
	lines := make([]string, 0, height)
	lines = append(lines, homeCell(m.homePaneTitle(pane, title, width, focused), width))
	for _, row := range rows {
		if len(lines) >= height {
			break
		}
		lines = append(lines, homeCell(row, width))
	}
	for len(lines) < height {
		lines = append(lines, homeCell("", width))
	}
	return lines
}

func (m model) homePaneTitle(pane int, title string, width int, focused bool) string {
	if focused {
		style := homeFocusRoot
		switch pane {
		case homePaneBackups:
			style = homeFocusBackup
		case homePaneActivity:
			style = homeFocusActivityTitle
		}
		return style.Width(width).Render(" " + strings.ToUpper(title) + " ")
	}
	return homeTitleStyle.Render(" " + title + " ")
}

func (m model) homeDetailsTitle() string {
	switch m.homeFocus {
	case homeFocusBackups:
		return "Details / selected backup"
	case homeFocusSystem:
		return "Details / system"
	default:
		return "Details / selected root"
	}
}

func (m model) homeOverviewDetailRows(width int) []string {
	switch m.homeFocus {
	case homeFocusBackups:
		return m.homeSelectedBackupRows(width)
	case homeFocusSystem:
		return m.homeSystemSummaryRows(width)
	default:
		return m.homeSelectedRootRows(width)
	}
}

func (m model) homeSelectedRootRows(width int) []string {
	if len(m.status.Roots) == 0 {
		return []string{
			homeMutedStyle.Render("Nothing selected."),
			homeMutedStyle.Render("Add a folder, then press b to back it up."),
		}
	}
	root := m.status.Roots[min(m.folderIndex, len(m.status.Roots)-1)]
	latest := m.selectedRootLatestBackup()
	lastBackup := "never"
	if latest.ID != "" {
		lastBackup = firstNonEmpty(compactSnapshotTime(latest.StartTime), latest.StartTime)
	}
	rows := []string{
		"Root: " + homeShortPath(root.Path),
		"Last backup: " + lastBackup,
		fmt.Sprintf("Snapshots: %d   Size: %s", root.Snapshots, formatBytes(root.SizeBytes)),
		"Protection: " + colorByStatus(root.Protection).Render(root.Protection),
		"Peers: " + m.homePeerCountLabel() + "   Receipts: " + fmt.Sprintf("%d", m.status.ReceiptCount),
		"Audits: " + m.homeAuditLabel(),
	}
	if root.ProtectionNote != "" {
		rows = append(rows, "Health: "+root.ProtectionNote)
	}
	if root.LastFailure != "" {
		rows = append(rows, warnStyle.Render("Last error: "+root.LastFailure))
	}
	for i, row := range rows {
		rows[i] = homeTruncateStyled(row, width)
	}
	return rows
}

func (m model) homeSelectedBackupRows(width int) []string {
	snapshot := m.selectedHomeSnapshot()
	if snapshot.ID == "" {
		return []string{
			homeMutedStyle.Render("No backup selected."),
			homeMutedStyle.Render("Select a root with snapshots first."),
		}
	}
	rows := []string{
		"Backup: " + m.homeSnapshotLabel(snapshot),
		"Snapshot ID: " + shortID(snapshot.ID),
		"Source: " + homeShortPath(snapshot.Source),
		"Started: " + firstNonEmpty(compactSnapshotTime(snapshot.StartTime), snapshot.StartTime),
		fmt.Sprintf("Files: %d   Dirs: %d   Size: %s", snapshot.Files, snapshot.Dirs, formatBytes(snapshot.SizeBytes)),
		"Restore target: " + defaultRestoreTarget(snapshot.ID),
	}
	for i, row := range rows {
		rows[i] = homeTruncateStyled(row, width)
	}
	return rows
}

func (m model) homeSystemSummaryRows(width int) []string {
	rows := []string{
		"Repository: " + m.homeRepoLabel(),
		"Storage: " + firstNonEmpty(m.status.StorageMode, "unknown"),
		"Daemon: " + m.homeDaemonLabel(),
		"Peers: " + m.homePeerCountLabel(),
		fmt.Sprintf("Shards: %d data + %d parity", m.status.DataShards, m.status.ParityShards),
		"Schedule: " + firstNonEmpty(m.summary.Schedule, m.status.Schedule, "unknown"),
		"Retention: " + firstNonEmpty(m.summary.Retention, m.status.Retention, "unknown"),
		"Config: " + firstNonEmpty(m.summary.Path, m.configPath, "missing"),
	}
	for i, row := range rows {
		rows[i] = homeTruncateStyled(row, width)
	}
	return rows
}

func (m model) homeActivityRows(width int) []string {
	rows := []string{}
	if m.op != nil {
		bar := m.progress
		bar.Width = max(12, width)
		rows = append(rows, homeOpStyle.Render(m.homeOperationDetail(width)))
		rows = append(rows, bar.ViewAs(progressPercent(m.op.event.BytesDone, m.op.event.BytesTotal)))
	}
	activity := append([]string(nil), m.activity...)
	if len(activity) == 0 && len(m.daemon.LastLines) > 0 {
		for _, line := range m.daemon.LastLines {
			activity = append(activity, "daemon "+line)
		}
	}
	for i := len(activity) - 1; i >= 0; i-- {
		rows = append(rows, homeMutedStyle.Render(middleTruncate(activity[i], width)))
	}
	if len(rows) == 0 {
		rows = append(rows, homeMutedStyle.Render("No activity yet."))
	}
	return rows
}

func (m model) homeFooter(width int) string {
	bindings := m.homeHelpBindings().ShortHelp()
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if !binding.Enabled() {
			continue
		}
		help := binding.Help()
		if help.Key == "" {
			continue
		}
		parts = append(parts, homeKeyStyle.Render(help.Key)+" "+help.Desc)
	}
	line := strings.Join(parts, "  ")
	if lipgloss.Width(line) <= width {
		return line
	}
	compact := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		if help.Key != "" {
			compact = append(compact, homeKeyStyle.Render(help.Key))
		}
	}
	return strings.Join(compact, " ")
}

func (m model) homeHelpBindings() homeHelpBindings {
	if m.homeMode == homeDetailHelp {
		return homeHelpBindings{
			short: []key.Binding{homeKeys.Back, homeKeys.Quit},
			full:  [][]key.Binding{{homeKeys.Back, homeKeys.Quit}},
		}
	}
	if m.homeMode != homeDetailOverview {
		parts := []key.Binding{homeKeys.Back}
		switch m.homeMode {
		case homeDetailRoot:
			parts = append(parts, homeKeys.Backup, homeKeys.Remove, homeKeys.Audit)
		case homeDetailBackup:
			parts = append(parts, homeKeys.Restore, homeKeys.Audit)
		case homeDetailSystem:
			parts = append(parts, homeKeys.Edit, homeKeys.Init, homeKeys.Start, homeKeys.Stop, homeKeys.Reset)
		}
		parts = append(parts, homeKeys.Help, homeKeys.Quit)
		return homeHelpBindings{short: parts, full: [][]key.Binding{parts}}
	}
	switch m.homeFocus {
	case homeFocusBackups:
		return homeHelpBindings{
			short: []key.Binding{homeKeys.Up, homeKeys.Down, homeKeys.Restore, homeKeys.Details, homeKeys.NextPane, homeKeys.Help, homeKeys.Quit},
			full:  [][]key.Binding{{homeKeys.Up, homeKeys.Down, homeKeys.Restore, homeKeys.Details}, {homeKeys.NextPane, homeKeys.PrevPane, homeKeys.PrevTab, homeKeys.NextTab}, {homeKeys.Help, homeKeys.Quit}},
		}
	case homeFocusSystem:
		return homeHelpBindings{
			short: []key.Binding{homeKeys.NextPane, homeKeys.Start, homeKeys.Stop, homeKeys.Audit, homeKeys.Password, homeKeys.Details, homeKeys.Help, homeKeys.Quit},
			full:  [][]key.Binding{{homeKeys.NextPane, homeKeys.PrevPane, homeKeys.Details}, {homeKeys.Start, homeKeys.Stop, homeKeys.Audit, homeKeys.Password}, {homeKeys.PrevTab, homeKeys.NextTab, homeKeys.Help, homeKeys.Quit}},
		}
	default:
		return homeHelpBindings{
			short: []key.Binding{homeKeys.Up, homeKeys.Down, homeKeys.Backup, homeKeys.Add, homeKeys.Remove, homeKeys.Details, homeKeys.NextPane, homeKeys.Help, homeKeys.Quit},
			full:  [][]key.Binding{{homeKeys.Up, homeKeys.Down, homeKeys.Details}, {homeKeys.Backup, homeKeys.Add, homeKeys.Remove}, {homeKeys.NextPane, homeKeys.PrevPane, homeKeys.PrevTab, homeKeys.NextTab}, {homeKeys.Help, homeKeys.Quit}},
		}
	}
}

func (m model) homeFrameHeight() int {
	height := m.tableHeight(8)
	if height < 16 {
		return 16
	}
	if height > 28 {
		return 28
	}
	return height
}

func (m model) homeDaemonLabel() string {
	if m.daemon.Running {
		return "running"
	}
	return "stopped"
}

func (m model) homePeerCountLabel() string {
	healthy := 0
	for _, peer := range m.status.Peers {
		if peer.Status == "healthy" {
			healthy++
		}
	}
	return fmt.Sprintf("%d/%d", healthy, len(m.status.Peers))
}

func (m model) homeRepoLabel() string {
	if m.status.Repository == "initialized" && m.status.RepositoryError == "" {
		return "ok"
	}
	return backupStoreLabel(m.status.Repository)
}

func (m model) homeAuditLabel() string {
	if m.status.AuditCount == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d records", m.status.AuditCount)
}

func (m model) homeOperationLabel() string {
	if m.op == nil {
		return "idle"
	}
	event := m.op.event
	label := strings.TrimSpace(event.Kind + " " + event.Phase)
	if label == "" {
		return "running"
	}
	return label
}

func (m model) homeOperationDetail(width int) string {
	event := m.op.event
	percent := int(progressPercent(event.BytesDone, event.BytesTotal) * 100)
	parts := []string{strings.TrimSpace(event.Kind + " " + event.Phase)}
	if parts[0] == "" {
		parts[0] = "operation running"
	}
	parts = append(parts, fmt.Sprintf("%d%%", percent))
	if event.SpeedBps > 0 {
		parts = append(parts, formatBytes(int64(event.SpeedBps))+"/s")
	}
	if event.ETA > 0 {
		parts = append(parts, "ETA "+etaLabel(event.ETA))
	}
	return middleTruncate(strings.Join(parts, "  "), width)
}

func (m model) homeSnapshotLabel(snapshot app.Snapshot) string {
	if snapshot.ID == "" {
		return "none"
	}
	if selected := m.selectedHomeSnapshot(); selected.ID == snapshot.ID && m.folderBackup == 0 {
		return "latest"
	}
	return shortID(snapshot.ID)
}

func (m model) homeDetailFrame(width int) string {
	frameHeight := m.homeFrameHeight()
	innerWidth := width - 2
	title := m.homeDetailTitle()
	m.homeDetail.Width = max(1, innerWidth)
	m.homeDetail.Height = max(1, frameHeight-2)
	m.homeDetail.SetContent(strings.Join(m.homeDetailRows(innerWidth-2), "\n"))
	lines := []string{m.homeDetailHeader(width, title)}
	for _, line := range strings.Split(m.homeDetail.View(), "\n") {
		lines = append(lines, homeBorderStyle.Render("│")+homeCell(line, innerWidth)+homeBorderStyle.Render("│"))
	}
	for len(lines) < frameHeight-1 {
		lines = append(lines, homeBorderStyle.Render("│")+homeCell("", innerWidth)+homeBorderStyle.Render("│"))
	}
	lines = append(lines, homeBorderStyle.Render("└"+strings.Repeat("─", innerWidth)+"┘"))
	return strings.Join(lines, "\n")
}

func (m model) homeDetailHeader(width int, title string) string {
	innerWidth := width - 2
	left := " p2pbk / " + title + " "
	right := " " + strings.Join([]string{
		"daemon: " + m.homeDaemonLabel(),
		"peers: " + m.homePeerCountLabel(),
		"repo: " + m.homeRepoLabel(),
	}, "  ") + " "
	space := innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 0 {
		right = " " + middleTruncate(strings.TrimSpace(right), max(0, innerWidth-lipgloss.Width(left)-2)) + " "
		space = innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	}
	return homeBorderStyle.Render("┌") + homeTitleStyle.Render(left) + homeBorderStyle.Render(strings.Repeat("─", max(0, space))) + homeMutedStyle.Render(right) + homeBorderStyle.Render("┐")
}

func (m model) homeDetailTitle() string {
	switch m.homeMode {
	case homeDetailRoot:
		return "root details"
	case homeDetailBackup:
		return "backup details"
	case homeDetailSystem:
		return "system"
	case homeDetailHelp:
		return "help"
	default:
		return "overview"
	}
}

func (m model) homeDetailRows(width int) []string {
	switch m.homeMode {
	case homeDetailRoot:
		return m.homeRootDetailRows(width)
	case homeDetailBackup:
		return m.homeBackupDetailRows(width)
	case homeDetailSystem:
		return m.homeSystemDetailRows(width)
	case homeDetailHelp:
		return m.homeHelpRows(width)
	default:
		return nil
	}
}

func (m model) homeRootDetailRows(width int) []string {
	if len(m.status.Roots) == 0 {
		return []string{homeMutedStyle.Render("No protected root selected.")}
	}
	root := m.status.Roots[min(m.folderIndex, len(m.status.Roots)-1)]
	rows := []string{
		"Root: " + homeShortPath(root.Path),
		"Status: " + colorByStatus(root.Protection).Render(root.Protection),
		"",
		"Path exists: " + boolWord(root.Kind != "missing", "yes", "no"),
		"Kind: " + root.Kind,
		fmt.Sprintf("Snapshots: %d", root.Snapshots),
		"Latest backup: " + m.folderLastBackup(root),
		"Size: " + formatBytes(root.SizeBytes),
		"Last success: " + firstNonEmpty(root.LastSuccess, "none"),
		"Last failure: " + firstNonEmpty(root.LastFailure, "none"),
		"",
		"Protection explanation",
		m.homeProtectionExplanation(root),
		"",
		"Storage policy",
		fmt.Sprintf("Data shards: %d", m.status.DataShards),
		fmt.Sprintf("Parity shards: %d", m.status.ParityShards),
		fmt.Sprintf("Receipts: %d", m.status.ReceiptCount),
		"Audits: " + m.homeAuditLabel(),
	}
	return homeTruncateRows(rows, width)
}

func (m model) homeBackupDetailRows(width int) []string {
	snapshot := m.selectedHomeSnapshot()
	if snapshot.ID == "" {
		return []string{homeMutedStyle.Render("No backup selected.")}
	}
	rows := []string{
		"Snapshot: " + snapshot.ID,
		"Source: " + homeShortPath(snapshot.Source),
		"Started: " + firstNonEmpty(snapshot.StartTime, "unknown"),
		"Description: " + firstNonEmpty(snapshot.Description, "none"),
		"",
		"Contents",
		fmt.Sprintf("Files: %d", snapshot.Files),
		fmt.Sprintf("Directories: %d", snapshot.Dirs),
		"Logical size: " + formatBytes(snapshot.SizeBytes),
		"",
		"Restore",
		"Default target: " + defaultRestoreTarget(snapshot.ID),
		"",
		"Distributed storage",
		fmt.Sprintf("Receipts: %d recorded", m.status.ReceiptCount),
		"Peers: " + m.homePeerCountLabel(),
		"Audit history: " + m.homeAuditLabel(),
	}
	return homeTruncateRows(rows, width)
}

func (m model) homeSystemDetailRows(width int) []string {
	rows := []string{
		"Repository",
		"Status: " + m.homeRepoLabel(),
		"Storage: " + firstNonEmpty(m.status.StorageMode, "unknown"),
		"Config: " + firstNonEmpty(m.summary.Path, m.configPath, "missing"),
		"",
		"Paths",
		"Data: " + firstNonEmpty(m.summary.DataDir, m.status.DataDir, "unknown"),
		"Store: " + firstNonEmpty(m.summary.StoreDir, m.status.StoreDir, "unknown"),
		"Cache: " + firstNonEmpty(m.summary.CacheDir, m.status.CacheDir, "unknown"),
		"",
		"Backup policy",
		"Schedule: " + firstNonEmpty(m.summary.Schedule, m.status.Schedule, "unknown"),
		"Retention: " + firstNonEmpty(m.summary.Retention, m.status.Retention, "unknown"),
		"Max protected bytes: " + formatBytes(m.summary.MaxProtectedBytes),
		fmt.Sprintf("Shards: %d data + %d parity", m.status.DataShards, m.status.ParityShards),
		"",
		"Networking",
		"Discovery: " + firstNonEmpty(m.summary.Discovery, "unknown"),
		"Protocol: " + firstNonEmpty(m.summary.ProtocolID, "unknown"),
		"Peers: " + m.homePeerCountLabel(),
	}
	rows = append(rows, m.homePeerDetailRows()...)
	rows = append(rows,
		"",
		"Service",
		"Daemon: "+m.homeDaemonLabel(),
		"LocalDB: "+firstNonEmpty(m.status.LocalDBStatus, "unknown"),
		"Next check: "+m.nextMaintenanceLabel(),
	)
	return homeTruncateRows(rows, width)
}

func (m model) homePeerDetailRows() []string {
	if len(m.status.Peers) == 0 {
		return []string{"Peer detail: no known peers yet"}
	}
	rows := []string{"Known peers"}
	limit := min(4, len(m.status.Peers))
	for i := 0; i < limit; i++ {
		peer := m.status.Peers[i]
		rows = append(rows, fmt.Sprintf("%s  %s  score %.1f  shards %d  failures %d",
			shortText(peer.ID, 12),
			firstNonEmpty(peer.Status, "unknown"),
			peer.Score,
			peer.StoredShards,
			peer.Failures,
		))
	}
	if len(m.status.Peers) > limit {
		rows = append(rows, fmt.Sprintf("... %d more peers", len(m.status.Peers)-limit))
	}
	return rows
}

func (m model) homeHelpRows(width int) []string {
	rows := []string{
		"Navigation",
		"tab/right: next pane",
		"shift+tab/left: previous pane",
		"up/down: select root or backup",
		"enter: open details for focused pane",
		"esc: back from details",
		"",
		"Root actions",
		"b: backup selected root",
		"a: add protected root",
		"d: remove selected root",
		"",
		"Backup actions",
		"r: restore selected backup",
		"u: run audit",
		"",
		"System actions",
		"p: enter password",
		"e: edit config",
		"i: initialize when setup is missing",
		"s/S: start or stop service",
		"x: reset local state from system details",
		"",
		"Legacy tabs",
		"[/]: switch to previous or next legacy tab while they still exist.",
	}
	return homeTruncateRows(rows, width)
}

func (m model) homeProtectionExplanation(root app.RootStatus) string {
	switch root.Protection {
	case "protected":
		return "Enough backup history and peer evidence exists for the configured policy."
	case "degraded":
		return "Distributed protection exists, but there are not enough healthy peers right now."
	case "local only":
		return "Backups exist locally, but no distributed shard placement is confirmed."
	case "offload unknown":
		return "Backups exist, but p2pbk has no shard receipts for distributed storage yet."
	case "unprotected":
		return "This root has no snapshot yet. Press b to create the first backup."
	case "missing":
		return "The configured path no longer exists on this machine."
	default:
		return firstNonEmpty(root.ProtectionNote, "No additional health detail is available.")
	}
}

func (m model) dashboardView() string {
	width := m.contentWidth()
	stacked := width < 96
	cardWidth := width
	if !stacked {
		cardWidth = (width - 4) / 3
	}

	protection := m.protectionCard(cardWidth)
	service := m.serviceCard(cardWidth)
	storage := m.storageCard(cardWidth)
	latest := m.latestBackupCard(width)

	sections := []string{}
	if stacked {
		sections = append(sections, protection, service, storage, latest)
	} else {
		sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, protection, "  ", service, "  ", storage), latest)
	}
	if m.op != nil {
		sections = append(sections, m.operationView())
	}
	if warning := m.dashboardWarning(); warning != "" {
		sections = append(sections, m.dashboardNoticeCard(width, warning))
	}
	sections = append(sections, m.recentActivityPanel(width, m.dashboardActivityHeight()))
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m model) protectionCard(width int) string {
	bodyWidth := cardBodyWidth(width)
	lines := []string{
		cardTitle.Render("Protection"),
		m.statusBadge(m.overallProtection()),
		middleTruncate(fmt.Sprintf("%d folders, %d backups", len(m.status.Roots), len(m.status.Snapshots)), bodyWidth),
		middleTruncate(m.peerSummary(), bodyWidth),
	}
	return renderCard(width, lines)
}

func (m model) serviceCard(width int) string {
	bodyWidth := cardBodyWidth(width)
	lines := []string{
		cardTitle.Render("Service"),
		m.statusBadge(serviceLabel(m.daemon.Running)),
		middleTruncate("next check "+m.nextMaintenanceLabel(), bodyWidth),
		middleTruncate(boolWord(len(m.daemon.LastLines) > 0, "log active", "no recent log"), bodyWidth),
	}
	return renderCard(width, lines)
}

func (m model) storageCard(width int) string {
	bodyWidth := cardBodyWidth(width)
	bar := m.progress
	bar.Width = max(18, bodyWidth)
	lines := []string{
		cardTitle.Render("Storage"),
		middleTruncate(m.spaceUsageLabel(), bodyWidth),
		bar.ViewAs(progressPercent(m.status.StorageBytes, m.status.MaxProtectedBytes)),
		middleTruncate(m.offloadLabel(), bodyWidth),
	}
	return renderCard(width, lines)
}

func (m model) latestBackupCard(width int) string {
	bodyWidth := cardBodyWidth(width)
	snapshot := m.latestBackup()
	lines := []string{cardTitle.Render("Latest backup")}
	if snapshot.ID == "" {
		lines = append(lines, "No backups yet", "Select a folder and press b")
	} else {
		sourceName := filepath.Base(filepath.Clean(snapshot.Source))
		lines = append(lines,
			middleTruncate(fmt.Sprintf("%s  %s  %d files", sourceName, snapshot.StartTime, snapshot.Files), bodyWidth),
			middleTruncate(snapshot.Source, bodyWidth),
		)
	}
	return renderCard(width, lines)
}

func (m model) dashboardNoticeCard(width int, notice string) string {
	return renderCard(width, []string{cardTitle.Render("Attention"), warnStyle.Render(middleTruncate(notice, cardBodyWidth(width)))})
}

func (m model) recentActivityPanel(width, maxLines int) string {
	bodyWidth := cardBodyWidth(width)
	lines := []string{cardTitle.Render("Recent activity")}
	activity := append([]string(nil), m.activity...)
	if len(activity) == 0 {
		activity = append(activity, "No activity yet.")
	}
	if maxLines < 1 {
		maxLines = 1
	}
	if len(activity) > maxLines {
		activity = activity[len(activity)-maxLines:]
	}
	for _, line := range activity {
		lines = append(lines, middleTruncate(line, bodyWidth))
	}
	return renderCard(width, lines)
}

func renderCard(width int, lines []string) string {
	if width < 34 {
		width = 34
	}
	bodyWidth := cardBodyWidth(width)
	out := make([]string, 0, len(lines)+1)
	for i, line := range lines {
		out = append(out, lipgloss.NewStyle().Width(bodyWidth).Render(line))
		if i == 0 {
			out = append(out, cardRule.Render(strings.Repeat("─", bodyWidth)))
		}
	}
	return cardStyle.Width(width - 2).Render(strings.Join(out, "\n"))
}

func cardBodyWidth(width int) int {
	if width < 34 {
		width = 34
	}
	return max(20, width-6)
}

func (m model) foldersView() string {
	if len(m.status.Roots) == 0 {
		return panelStyle.Render("No protected folders. Press a to add one, or open Settings to initialize.")
	}
	width := m.contentWidth()
	height := m.fullTableHeight()
	stacked := width < 96
	leftWidth := (width - 4) / 2
	rightWidth := width - leftWidth - 4
	if stacked {
		leftWidth = width
		rightWidth = width
		height = max(4, height/2)
	}
	folderPathWidth := max(8, leftWidth-44)
	folderRows := []table.Row{}
	for i, root := range m.status.Roots {
		folderRows = append(folderRows, table.Row{
			selectMarker(i, m.folderIndex),
			root.Kind,
			root.Protection,
			fmt.Sprintf("%d", root.Snapshots),
			formatBytes(root.SizeBytes),
			middleTruncate(root.Path, folderPathWidth),
		})
	}
	folders := renderTable([]table.Column{
		{Title: "", Width: 1},
		{Title: "Type", Width: 5},
		{Title: "Protection", Width: 13},
		{Title: "Bks", Width: 4},
		{Title: "Size", Width: 9},
		{Title: "Folder", Width: folderPathWidth},
	}, folderRows, m.folderIndex, m.folderFocus == folderPaneFolders, height)

	descriptionWidth := max(10, rightWidth-43)
	backupRows := []table.Row{}
	for i, snapshot := range m.folderBackups() {
		backupRows = append(backupRows, table.Row{
			selectMarker(i, m.folderBackup),
			middleTruncate(snapshot.ID, 10),
			middleTruncate(snapshot.StartTime, 16),
			fmt.Sprintf("%d", snapshot.Files),
			middleTruncate(snapshot.Description, descriptionWidth),
		})
	}
	if len(backupRows) == 0 {
		backupRows = append(backupRows, table.Row{"", "", "", "", "No backups"})
	}
	backups := renderTable([]table.Column{
		{Title: "", Width: 1},
		{Title: "ID", Width: 10},
		{Title: "Started", Width: 16},
		{Title: "Files", Width: 6},
		{Title: "Description", Width: descriptionWidth},
	}, backupRows, m.folderBackup, m.folderFocus == folderPaneBackups && len(m.folderBackups()) > 0, height)

	details := m.selectedFolderDetails()

	if stacked {
		return lipgloss.JoinVertical(lipgloss.Left, folders, backups, details)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, folders, "  ", backups), details)
}

func (m model) backupsView() string {
	if len(m.status.Snapshots) == 0 {
		return panelStyle.Render("No backups yet. Select a folder and press b to create one.")
	}
	width := m.contentWidth()
	stacked := width < 96
	tableWidth := width
	detailWidth := width
	if !stacked {
		tableWidth = (width * 2) / 3
		detailWidth = width - tableWidth - 4
	}
	sourceWidth := max(16, tableWidth-68)
	rows := []table.Row{}
	for i, snapshot := range m.status.Snapshots {
		rows = append(rows, table.Row{
			selectMarker(i, m.selected),
			middleTruncate(snapshot.ID, 12),
			middleTruncate(snapshot.StartTime, 16),
			formatBytes(snapshot.SizeBytes),
			fmt.Sprintf("%d", snapshot.Files),
			middleTruncate(snapshot.Source, sourceWidth),
		})
	}
	tableView := renderTable([]table.Column{
		{Title: "", Width: 1},
		{Title: "ID", Width: 12},
		{Title: "Started", Width: 16},
		{Title: "Size", Width: 9},
		{Title: "Files", Width: 8},
		{Title: "Source", Width: sourceWidth},
	}, rows, m.selected, true, m.fullTableHeight())
	detail := m.backupDetailView(detailWidth)
	if stacked {
		return lipgloss.JoinVertical(lipgloss.Left, tableView, detail)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tableView, "  ", detail)
}

func (m model) settingsView() string {
	header := m.settingsTabsView()
	var body string
	switch m.settingsTab {
	case settingsService:
		body = m.serviceSettingsView()
	case settingsAdvanced:
		body = m.advancedSettingsView()
	default:
		body = m.configSettingsView()
	}
	return header + "\n\n" + body
}

func (m model) settingsTabsView() string {
	parts := make([]string, len(settingsSections))
	for i, name := range settingsSections {
		if i == m.settingsTab {
			parts[i] = activeStyle.Render("[" + name + "]")
		} else {
			parts[i] = mutedStyle.Render(" " + name + " ")
		}
	}
	return strings.Join(parts, "  ")
}

func (m model) configSettingsView() string {
	width := m.contentWidth()
	columns := fieldValueColumns(width, "Setting", "Value")
	valueWidth := columns[1].Width
	rows := []table.Row{
		{"Config file", middleTruncate(firstNonEmpty(m.summary.Path, "missing"), valueWidth)},
		{"Display name", middleTruncate(m.summary.DisplayName, valueWidth)},
		{"Data directory", middleTruncate(m.summary.DataDir, valueWidth)},
		{"Store directory", middleTruncate(m.summary.StoreDir, valueWidth)},
		{"Cache directory", middleTruncate(m.summary.CacheDir, valueWidth)},
		{"Offer space", formatBytes(m.summary.OfferBytes)},
		{"Max protected", formatBytes(m.summary.MaxProtectedBytes)},
		{"Schedule", m.summary.Schedule},
		{"Retention", m.summary.Retention},
		{"Discovery", m.summary.Discovery},
		{"Bootstrap peers", fmt.Sprintf("%d", len(m.summary.BootstrapPeers))},
	}
	return renderTable(columns, rows, m.selected, true, m.fullTableHeight())
}

func (m model) serviceSettingsView() string {
	width := m.contentWidth()
	columns := fieldValueColumns(width, "Service", "Value")
	valueWidth := columns[1].Width
	rows := []table.Row{
		{"Status", serviceLabel(m.daemon.Running)},
		{"Config file", middleTruncate(firstNonEmpty(m.configPath, "missing"), valueWidth)},
		{"Log path", middleTruncate(firstNonEmpty(m.daemon.LogPath, "unknown"), valueWidth)},
		{"Note", middleTruncate(m.daemon.Note, valueWidth)},
	}
	for _, line := range m.daemon.LastLines {
		rows = append(rows, table.Row{"Log", middleTruncate(line, valueWidth)})
	}
	return renderTable(columns, rows, 0, false, m.fullTableHeight())
}

func (m model) advancedSettingsView() string {
	width := m.contentWidth()
	columns := fieldValueColumns(width, "Advanced", "Value")
	valueWidth := columns[1].Width
	rows := []table.Row{
		{"Backup store", middleTruncate(backupStoreLabel(m.status.Repository), valueWidth)},
		{"Store config", middleTruncate(firstNonEmpty(m.repo.ConfigPath, m.status.RepositoryPath), valueWidth)},
		{"Storage", middleTruncate(firstNonEmpty(m.repo.StorageType, m.status.StorageMode), valueWidth)},
		{"LocalDB", middleTruncate(firstNonEmpty(m.status.LocalDBStatus, "available"), valueWidth)},
		{"Unique ID", middleTruncate(m.repo.UniqueID, valueWidth)},
		{"Peers", fmt.Sprintf("%d known", len(m.status.Peers))},
		{"Receipts", fmt.Sprintf("%d", m.status.ReceiptCount)},
		{"Audits", fmt.Sprintf("%d", m.status.AuditCount)},
		{"Data shards", fmt.Sprintf("%d data + %d parity", m.status.DataShards, m.status.ParityShards)},
		{"Next full", formatTime(m.maint.NextFull)},
		{"Next quick", formatTime(m.maint.NextQuick)},
	}
	return renderTable(columns, rows, 0, false, m.fullTableHeight())
}

func (m model) backupDetailView(width int) string {
	if len(m.status.Snapshots) == 0 {
		return panelStyle.Render("No backup selected.")
	}
	if width < 40 {
		width = 40
	}
	snapshot := m.status.Snapshots[min(m.selected, len(m.status.Snapshots)-1)]
	valueWidth := max(20, width-18)
	rows := []table.Row{
		{"Backup ID", middleTruncate(snapshot.ID, valueWidth)},
		{"Started", middleTruncate(snapshot.StartTime, valueWidth)},
		{"Source", middleTruncate(snapshot.Source, valueWidth)},
		{"Size", formatBytes(snapshot.SizeBytes)},
		{"Files", fmt.Sprintf("%d", snapshot.Files)},
		{"Dirs", fmt.Sprintf("%d", snapshot.Dirs)},
		{"Description", middleTruncate(snapshot.Description, valueWidth)},
		{"Restore to", middleTruncate(defaultRestoreTarget(snapshot.ID), valueWidth)},
	}
	return renderTable([]table.Column{{Title: "Detail", Width: 14}, {Title: "Value", Width: valueWidth}}, rows, 0, false, m.fullTableHeight())
}

func (m model) selectedFolderDetails() string {
	if len(m.status.Roots) == 0 {
		return ""
	}
	root := m.status.Roots[min(m.folderIndex, len(m.status.Roots)-1)]
	lines := []string{
		"Selected: " + middleTruncate(root.Path, max(20, m.contentWidth()-12)),
		"Protection: " + root.Protection + "  " + root.ProtectionNote,
		"Size: " + formatBytes(root.SizeBytes),
		"Last backup: " + m.folderLastBackup(root),
	}
	if root.LastFailure != "" {
		lines = append(lines, "Last error: "+root.LastFailure)
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m model) dashboardActivityHeight() int {
	return m.tableHeight(18)
}

func (m model) dashboardWarning() string {
	if m.err != nil {
		return m.friendlyError(m.err)
	}
	if notice := m.setupNotice(); notice != "" {
		return notice
	}
	if m.message != "" {
		return m.message
	}
	return ""
}

func (m model) setupNotice() string {
	if m.configPath == "" {
		return "Setup needed: open Settings and press i to initialize p2pbk before backing up."
	}
	if m.status.Repository == "not initialized" {
		return "Setup needed: open Settings and press i to initialize the backup store, then add a folder or press b to back up."
	}
	if strings.Contains(strings.ToLower(m.status.RepositoryError), "password") {
		return "Backups are locked: press p to enter the p2pbk password for this session."
	}
	if needsBackupStoreInit(m.status.RepositoryError) {
		return "Setup needed: open Settings and press i to initialize the backup store before backing up."
	}
	return ""
}

func (m model) friendlyError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	lower := strings.ToLower(message)
	if strings.Contains(lower, "repository password") {
		return "p2pbk password is required. Press p to enter it for this session."
	}
	if needsBackupStoreInit(message) {
		return "Backup store is not initialized yet. Open Settings and press i to initialize it before backing up."
	}
	return message
}

func needsBackupStoreInit(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "repository is not initialized") ||
		strings.Contains(lower, "repository is not initialized or connected") ||
		strings.Contains(lower, "repository not connected")
}

func (m model) overallProtection() string {
	if len(m.status.Roots) == 0 {
		return "no folders"
	}
	worst := "protected"
	for _, root := range m.status.Roots {
		switch root.Protection {
		case "missing", "unprotected":
			return root.Protection
		case "degraded", "offload unknown", "local only":
			worst = root.Protection
		}
	}
	return worst
}

func (m model) offloadLabel() string {
	if m.status.StorageMode == "distributed" {
		return fmt.Sprintf("distributed, %d peers", len(m.status.Peers))
	}
	return m.status.StorageMode
}

func (m model) peerSummary() string {
	total := len(m.status.Peers)
	if total == 0 {
		return "No peers yet"
	}
	healthy := 0
	for _, peer := range m.status.Peers {
		if peer.Status == "healthy" {
			healthy++
		}
	}
	if total == 1 {
		return fmt.Sprintf("1 peer, %d healthy", healthy)
	}
	return fmt.Sprintf("%d peers, %d healthy", total, healthy)
}

func (m model) spaceUsageLabel() string {
	used := formatBytes(m.status.StorageBytes)
	if m.status.MaxProtectedBytes <= 0 {
		return used + " used"
	}
	return used + " / " + formatBytes(m.status.MaxProtectedBytes)
}

func (m model) latestBackup() app.Snapshot {
	if len(m.status.Snapshots) == 0 {
		return app.Snapshot{}
	}
	return m.status.Snapshots[0]
}

func (m model) selectedRootLatestBackup() app.Snapshot {
	for _, snapshot := range m.folderBackups() {
		return snapshot
	}
	return app.Snapshot{}
}

func (m model) selectedHomeSnapshot() app.Snapshot {
	snapshots := m.folderBackups()
	if len(snapshots) == 0 {
		return app.Snapshot{}
	}
	index := min(m.folderBackup, len(snapshots)-1)
	if index < 0 {
		index = 0
	}
	return snapshots[index]
}

func (m model) latestBackupLabel() string {
	snapshot := m.latestBackup()
	if snapshot.ID == "" {
		return "none"
	}
	source := filepath.Base(filepath.Clean(snapshot.Source))
	return strings.TrimSpace(snapshot.StartTime + " " + source)
}

func (m model) nextMaintenanceLabel() string {
	next := formatTime(m.maint.NextQuick)
	if next == "unknown" {
		next = formatTime(m.maint.NextFull)
	}
	return next
}

func serviceLabel(running bool) string {
	if running {
		return "running"
	}
	return "stopped or unknown"
}

func backupStoreLabel(status string) string {
	if status == "initialized" {
		return "initialized"
	}
	if status == "" {
		return "not initialized"
	}
	return status
}

func (m model) statusBadge(value string) string {
	style := badgeStyle.Foreground(cyberText).Background(cyberPanel)
	switch value {
	case "protected", "running", "initialized":
		style = badgeStyle.Foreground(cyberBg).Background(cyberGreen)
	case "local only", "offload unknown", "degraded", "stopped or unknown", "no folders":
		style = badgeStyle.Foreground(cyberBg).Background(cyberAmber)
	case "missing", "unprotected", "not initialized":
		style = badgeStyle.Foreground(cyberBg).Background(cyberRed)
	}
	return style.Render(value)
}

func (m model) operationView() string {
	event := m.op.event
	bar := m.progress
	bar.Width = m.operationProgressWidth()
	lines := []string{
		fmt.Sprintf("%s %s", event.Kind, event.Phase),
		bar.ViewAs(progressPercent(event.BytesDone, event.BytesTotal)),
		fmt.Sprintf("Data: %s / %s", formatBytes(event.BytesDone), totalLabel(event.BytesTotal)),
		fmt.Sprintf("Speed: %s/s  ETA: %s", formatBytes(int64(event.SpeedBps)), etaLabel(event.ETA)),
	}
	if event.Source != "" {
		lines = append(lines, "Source: "+event.Source)
	}
	if event.Target != "" {
		lines = append(lines, "Target: "+event.Target)
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m model) helpView() string {
	parts := []string{"tab switch", "p password"}
	rRestores := (m.tab == tabBackups && len(m.status.Snapshots) > 0) ||
		(m.tab == tabFolders && m.folderFocus == folderPaneBackups && len(m.folderBackups()) > 0) ||
		(m.tab == tabHome && m.selectedRootLatestBackup().ID != "") ||
		(m.tab == tabDashboard && len(m.status.Snapshots) > 0)
	if !rRestores {
		parts = append(parts, "r refresh")
	}
	if m.maxSelected() > 0 {
		parts = append(parts, "up/down select")
	}
	switch m.tab {
	case tabHome:
		parts = append(parts, "a add folder")
		if m.op == nil && len(m.status.Roots) > 0 {
			parts = append(parts, "b backup")
		}
		if m.op == nil && rRestores {
			parts = append(parts, "r restore")
		}
		if m.op == nil {
			parts = append(parts, "s start service", "S stop service")
		}
	case tabDashboard:
		parts = append(parts, "a add folder")
		if m.op == nil && len(m.status.Roots) > 0 {
			parts = append(parts, "b backup")
		}
		if m.op == nil && rRestores {
			parts = append(parts, "r restore")
		}
		if m.op == nil {
			parts = append(parts, "s start service", "S stop service")
		}
	case tabFolders:
		parts = append(parts, "left/right pane", "a add folder")
		if m.op == nil && len(m.status.Roots) > 0 {
			parts = append(parts, "b backup", "d remove")
		}
		if m.op == nil && rRestores {
			parts = append(parts, "r restore")
		}
	case tabBackups:
		if m.op == nil && rRestores {
			parts = append(parts, "r restore")
		}
	case tabSettings:
		parts = append(parts, "left/right section")
		switch m.settingsTab {
		case settingsConfig:
			parts = append(parts, "e edit config")
			if m.configPath == "" || m.status.Repository == "not initialized" {
				parts = append(parts, "i initialize")
			}
		case settingsService:
			if m.op == nil {
				parts = append(parts, "s start service", "S stop service")
			}
		case settingsAdvanced:
			parts = append(parts, "x reset")
		}
		if m.op == nil {
			parts = append(parts, "u audit")
		}
	}
	if m.op == nil && m.tab != tabSettings {
		parts = append(parts, "u audit")
	} else if m.op != nil {
		parts = append(parts, "c cancel")
	}
	parts = append(parts, "q quit")
	return mutedStyle.Render(strings.Join(parts, "  "))
}

func (m model) maxSelected() int {
	switch m.tab {
	case tabHome:
		return max(0, len(m.status.Roots)-1)
	case tabFolders:
		if m.folderFocus == folderPaneBackups {
			return max(0, len(m.folderBackups())-1)
		}
		return max(0, len(m.status.Roots)-1)
	case tabBackups:
		return max(0, len(m.status.Snapshots)-1)
	case tabSettings:
		if m.settingsTab == settingsConfig {
			return 10
		}
		return 0
	default:
		return 0
	}
}

func (m *model) moveFolderSelection(delta int) {
	if m.folderFocus == folderPaneBackups {
		next := m.folderBackup + delta
		if next < 0 {
			next = 0
		}
		if maxIndex := len(m.folderBackups()) - 1; next > maxIndex {
			next = max(0, maxIndex)
		}
		m.folderBackup = next
		return
	}
	m.moveRootSelection(delta)
}

func (m *model) moveRootSelection(delta int) {
	next := m.folderIndex + delta
	if next < 0 {
		next = 0
	}
	if maxIndex := len(m.status.Roots) - 1; next > maxIndex {
		next = max(0, maxIndex)
	}
	if next != m.folderIndex {
		m.folderIndex = next
		m.folderBackup = 0
	}
}

func (m model) folderBackups() []app.Snapshot {
	if len(m.status.Roots) == 0 || m.folderIndex >= len(m.status.Roots) {
		return nil
	}
	root := m.status.Roots[m.folderIndex].Path
	out := []app.Snapshot{}
	for _, snapshot := range m.status.Snapshots {
		if snapshotMatchesRoot(snapshot, root) {
			out = append(out, snapshot)
		}
	}
	return out
}

func (m model) folderLastBackup(root app.RootStatus) string {
	if root.LastSuccess != "" {
		return root.LastSuccess
	}
	var latest string
	for _, snapshot := range m.status.Snapshots {
		if snapshotMatchesRoot(snapshot, root.Path) && snapshot.StartTime > latest {
			latest = snapshot.StartTime
		}
	}
	return firstNonEmpty(latest, "never")
}

func snapshotMatchesRoot(snapshot app.Snapshot, root string) bool {
	clean := filepath.Clean(root)
	base := filepath.Base(clean)
	source := filepath.Clean(snapshot.Source)
	return strings.Contains(source, clean) || strings.Contains(source, base)
}

func (m model) contentWidth() int {
	if m.width <= 0 {
		return 100
	}
	width := m.width - 4
	if width < 40 {
		return 40
	}
	return width
}

func (m model) tableHeight(extraReserved int) int {
	height := m.height
	if height <= 0 {
		height = 28
	}
	out := height - extraReserved
	if out < 4 {
		return 4
	}
	return out
}

func fieldValueColumns(width int, fieldTitle, valueTitle string) []table.Column {
	if width < 44 {
		width = 44
	}
	fieldWidth := 18
	if width < 70 {
		fieldWidth = 14
	}
	valueWidth := width - fieldWidth - 5
	if valueWidth < 20 {
		valueWidth = 20
	}
	return []table.Column{{Title: fieldTitle, Width: fieldWidth}, {Title: valueTitle, Width: valueWidth}}
}

func (m model) fullTableHeight() int {
	reserved := 10
	if m.op != nil {
		reserved += 7
	}
	return m.tableHeight(reserved)
}

func (m model) operationProgressWidth() int {
	width := m.contentWidth() - 8
	if width < 20 {
		return 20
	}
	return width
}

func (m *model) addActivity(line string) {
	if line == "" {
		return
	}
	m.activity = append(m.activity, time.Now().Format("15:04:05")+" "+line)
}

func (m *model) openAddFolder() {
	m.form = addAssetForm()
	m.picker = m.newPickerForActiveField()
}

func fetchStatus(cfg config.Config, configPath, password string) tea.Cmd {
	return func() tea.Msg {
		status, err := app.CollectStatus(context.Background(), cfg, configPath, password)
		summary, summaryErr := app.SummarizeConfig(cfg, configPath)
		if err == nil {
			err = summaryErr
		}
		var repo app.RepositoryStatus
		var maint app.MaintenanceStatus
		if password != "" && status.Repository == "initialized" {
			repo, _ = app.GetRepositoryStatus(context.Background(), cfg, configPath, password)
			maint, _ = app.GetMaintenanceStatus(context.Background(), cfg, configPath, password)
		}
		return statusMsg{
			status:  status,
			repo:    repo,
			maint:   maint,
			daemon:  app.CheckDaemonStatus(context.Background(), cfg, configPath),
			summary: summary,
			err:     err,
		}
	}
}

func (m *model) startBackup(root string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan app.OperationEvent, 8)
	m.op = &operationState{cancel: cancel, events: events}
	m.addActivity("backup started: " + root)
	go app.BackupRootWithEvents(ctx, app.BackupRequest{
		Config:      m.cfg,
		ConfigPath:  m.configPath,
		Password:    m.password,
		Source:      root,
		Description: "tui",
	}, events)
	return waitOperation(events)
}

func (m *model) startRestore(snapshotID, target string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan app.OperationEvent, 8)
	m.op = &operationState{cancel: cancel, events: events}
	m.addActivity("restore started: " + snapshotID)
	go app.RestoreSnapshotWithEvents(ctx, app.RestoreRequest{
		Config:     m.cfg,
		ConfigPath: m.configPath,
		Password:   m.password,
		Snapshot:   snapshotID,
		Target:     target,
	}, events)
	return waitOperation(events)
}

func waitOperation(events <-chan app.OperationEvent) tea.Cmd {
	return func() tea.Msg {
		event := <-events
		return opMsg(event)
	}
}

func auditCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		result, err := app.RunAudit(context.Background(), app.AuditRequest{Config: cfg, Limit: 25, Timeout: 2 * time.Second})
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{message: fmt.Sprintf("audit checked=%d ok=%d failed=%d", result.Checked, result.OK, result.Failed)}
	}
}

func startServiceCmd(configPath, password string) tea.Cmd {
	return func() tea.Msg {
		if configPath == "" {
			return actionMsg{err: fmt.Errorf("config path is required")}
		}
		if err := app.StartDaemon(configPath, password); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{message: "service start requested"}
	}
}

func stopServiceCmd(configPath string) tea.Cmd {
	return func() tea.Msg {
		if err := app.StopDaemon(configPath); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{message: "service stop requested"}
	}
}

func tick() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg { return t })
}

func clearMessage() tea.Cmd {
	return tea.Tick(2500*time.Millisecond, func(time.Time) tea.Msg { return clearMessageMsg{} })
}

func colorByStatus(status string) lipgloss.Style {
	switch status {
	case "protected", "healthy":
		return successStyle
	case "degraded", "suspect", "offload unknown", "local only", "new":
		return warnStyle
	case "missing", "unprotected", "blocked":
		return errorStyle
	default:
		return lipgloss.NewStyle().Foreground(cyberText)
	}
}

func selectPrefix(i, selected int) string {
	if i == selected {
		return ">"
	}
	return " "
}

func selectMarker(i, selected int) string {
	if i == selected {
		return ">"
	}
	return ""
}

func renderTable(columns []table.Column, rows []table.Row, cursor int, selectable bool, maxHeight int) string {
	height := len(rows)
	if height < 1 {
		height = 1
	}
	if maxHeight <= 0 {
		maxHeight = 14
	}
	if height > maxHeight {
		height = maxHeight
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(selectable),
		table.WithHeight(height),
	)
	if selectable && cursor >= 0 && cursor < len(rows) {
		t.SetCursor(cursor)
	}
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(cyberViolet).
		BorderBottom(true).
		Bold(true).
		Foreground(cyberFocus)
	if selectable {
		styles.Selected = styles.Selected.
			Foreground(cyberText).
			Background(cyberRow).
			Bold(false)
	} else {
		styles.Selected = lipgloss.NewStyle()
	}
	t.SetStyles(styles)
	return panelStyle.Render(t.View())
}

func homeCell(value string, width int) string {
	if lipgloss.Width(value) > width {
		value = homeTruncateStyled(value, width)
	}
	return homePad(value, width)
}

func homeTruncateRows(rows []string, width int) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = homeTruncateStyled(row, width)
	}
	return out
}

func homePad(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func homeTruncateStyled(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	plain := stripANSI(value)
	return middleTruncate(plain, width)
}

func stripANSI(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch == '\x1b' {
			if i+1 < len(value) && value[i+1] == '[' {
				i += 2
				for i < len(value) {
					if value[i] >= '@' && value[i] <= '~' {
						break
					}
					i++
				}
			} else if i+1 < len(value) {
				i++
			}
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func homeShortPath(path string) string {
	if path == "" {
		return "unknown"
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		cleanHome := filepath.Clean(home)
		cleanPath := filepath.Clean(path)
		if cleanPath == cleanHome {
			return "~"
		}
		if strings.HasPrefix(cleanPath, cleanHome+string(os.PathSeparator)) {
			return "~" + strings.TrimPrefix(cleanPath, cleanHome)
		}
	}
	return filepath.Clean(path)
}

func compactSnapshotTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			now := time.Now()
			if parsed.Year() == now.Year() && parsed.YearDay() == now.YearDay() {
				return parsed.Format("15:04")
			}
			if parsed.Year() == now.Year() {
				return parsed.Format("Jan 02")
			}
			return parsed.Format("2006-01-02")
		}
	}
	if len(value) >= 16 && value[4] == '-' && value[7] == '-' {
		return value[:16]
	}
	return middleTruncate(value, 12)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func shortID(id string) string {
	return shortText(id, 12)
}

func shortText(value string, length int) string {
	if len(value) <= length {
		return value
	}
	return value[:length]
}

func middleTruncate(value string, length int) string {
	if length <= 0 {
		return ""
	}
	if len(value) <= length {
		return value
	}
	if length <= 3 {
		return value[:length]
	}
	left := (length - 3) / 2
	right := length - 3 - left
	return value[:left] + "..." + value[len(value)-right:]
}

func boolWord(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Format("2006-01-02 15:04")
}

func etaLabel(eta time.Duration) string {
	if eta <= 0 {
		return "unknown"
	}
	return eta.Round(time.Second).String()
}

func totalLabel(total int64) string {
	if total <= 0 {
		return "unknown"
	}
	return formatBytes(total)
}

func progressPercent(done, total int64) float64 {
	if total <= 0 {
		return 0
	}
	percent := float64(done) / float64(total)
	if percent < 0 {
		return 0
	}
	if percent > 1 {
		return 1
	}
	return percent
}

func defaultRestoreTarget(snapshotID string) string {
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Join("restore", shortID(snapshotID))
	}
	return filepath.Join(wd, "restore", shortID(snapshotID))
}
