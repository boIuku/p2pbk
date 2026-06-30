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
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"p2pbk/internal/app"
	"p2pbk/internal/config"
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

const (
	homeDetailLabelWidth = 14
	homeMetricLabelWidth = homeDetailLabelWidth
)

type model struct {
	cfg        config.Config
	configPath string
	password   string

	status               app.Status
	maint                app.MaintenanceStatus
	daemon               app.DaemonStatus
	summary              app.ConfigSummary
	repoDetailsLoading   bool
	repoDetailsRequested bool

	homeFocus    int
	homeMode     int
	width        int
	height       int
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
	maint   app.MaintenanceStatus
	daemon  app.DaemonStatus
	summary app.ConfigSummary
	err     error
}

type repoDetailsMsg struct {
	snapshots []app.Snapshot
	maint     app.MaintenanceStatus
	err       error
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
		{k.Help, k.Quit},
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

type homeMetric struct {
	name  string
	value string
}

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
	cyberRow    = lipgloss.Color("#183041")

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(cyberBg).Background(cyberFocus).Padding(0, 1)
	panelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cyberPanel).Padding(0, 1)
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
	homeOpStyle            = lipgloss.NewStyle().Bold(true).Foreground(cyberBlue)
)

func Run(cfg config.Config, configPath, password string) error {
	bar := progress.New(progress.WithGradient("#7CF7EA", "#58B7FF"))
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(cyberFocus2)))
	m := model{cfg: cfg, configPath: configPath, password: password, loading: true, progress: bar, spinner: spin}
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
		if next, cmd, handled := m.updateHomeKey(msg); handled {
			return next, cmd
		}
		if msg.String() == "esc" {
			if m.op != nil {
				m.op.cancel()
			}
			return m, tea.Quit
		}
	case statusMsg:
		m.loading = false
		m.status = msg.status
		m.maint = msg.maint
		m.daemon = msg.daemon
		m.summary = msg.summary
		m.err = msg.err
		m.folderIndex = min(m.folderIndex, max(0, len(m.status.Roots)-1))
		m.folderBackup = min(m.folderBackup, max(0, len(m.folderBackups())-1))
		m.syncHomeComponents()
		if m.repoDetailsRequested && m.password != "" && m.status.Repository == "initialized" {
			m.repoDetailsRequested = false
			m.repoDetailsLoading = true
			return m, fetchRepositoryDetails(m.cfg, m.configPath, m.password)
		}
		if m.repoDetailsRequested {
			m.repoDetailsRequested = false
			m.repoDetailsLoading = false
		}
	case repoDetailsMsg:
		m.repoDetailsLoading = false
		if msg.err != nil {
			m.status.RepositoryError = msg.err.Error()
		} else {
			app.ApplySnapshots(&m.status, msg.snapshots)
			m.maint = msg.maint
			m.status.RepositoryError = ""
		}
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
	body.WriteString("\n")
	body.WriteString(m.homeView())
	body.WriteString("\n\n")
	if m.err != nil {
		body.WriteString(errorStyle.Render(m.friendlyError(m.err)) + "\n")
	} else if notice := m.setupNotice(); notice != "" {
		body.WriteString(warnStyle.Render(notice) + "\n")
	} else if m.message != "" {
		body.WriteString(successStyle.Render(m.message) + "\n")
	}
	return body.String()
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

	listWidth := max(1, leftWidth-2)
	_ = m.homeRoots.SetItems(m.homeRootItems(listWidth))
	_ = m.homeBackups.SetItems(m.homeBackupItems(listWidth))
	m.homeRoots.Select(min(m.folderIndex, max(0, len(m.status.Roots)-1)))
	m.homeBackups.Select(min(m.folderBackup, max(0, len(m.folderBackups())-1)))
	m.homeDetail.SetContent(strings.Join(m.homeOverviewDetailRows(m.homeDetail.Width), "\n"))
	m.homeActivity.SetContent(strings.Join(m.homeActivityRows(m.homeActivity.Width), "\n"))
}

func (m model) homeLayout(width int) (leftWidth, rightWidth, topHeight, bottomHeight int) {
	frameHeight := m.homeFrameHeight()
	bodyHeight := frameHeight - 2
	innerWidth := width - 2
	leftWidth = max(34, min(46, innerWidth*45/100))
	rightWidth = innerWidth - leftWidth - 1
	topHeight = max(6, (bodyHeight-1)/2)
	bottomHeight = bodyHeight - topHeight - 1
	return leftWidth, rightWidth, topHeight, bottomHeight
}

func (m model) homeRootItems(width int) []list.Item {
	items := make([]list.Item, 0, len(m.status.Roots))
	for _, root := range m.status.Roots {
		items = append(items, homeListItem{title: m.homeRootListRow(root, width)})
	}
	if len(items) == 0 {
		items = append(items, homeListItem{title: "No protected folders yet"})
	}
	return items
}

func (m model) homeRootListRow(root app.RootStatus, width int) string {
	if width <= 0 {
		width = 1
	}
	lastBackup := compactRootBackupTime(m.folderLastBackup(root))
	status := m.homeRootSmartStatus(root)
	statusWidth := max(10, lipgloss.Width(stripANSI(status)))
	lastWidth := 10
	pathWidth, _ := listRowColumnWidths(width, 10, lastWidth, statusWidth)
	path := homePad(clipText(filepath.Base(filepath.Clean(root.Path)), pathWidth), pathWidth)
	meta := homeCell(lastBackup, lastWidth) + "  " + homeCell(status, statusWidth)
	return clipStyledText(path+"  "+meta, width)
}

func (m model) homeRootSmartStatus(root app.RootStatus) string {
	if root.Kind == "missing" || root.Protection == "missing" {
		return errorStyle.Render("missing")
	}
	if root.Snapshots == 0 {
		return warnStyle.Render("not backed up")
	}
	if m.status.StorageMode == "distributed" {
		return colorByStatus(root.Protection).Render(root.Protection)
	}
	return warnStyle.Render("local")
}

func (m model) homeBackupItems(width int) []list.Item {
	snapshots := m.folderBackups()
	items := make([]list.Item, 0, len(snapshots))
	for i, snapshot := range snapshots {
		label := shortID(snapshot.ID)
		if i == 0 {
			label = "latest"
		}
		items = append(items, homeListItem{title: homeBackupListRow(label, snapshot, width)})
	}
	if len(items) == 0 {
		if m.repoDetailsLoading {
			items = append(items, homeListItem{title: "Calculating snapshots..."})
		} else {
			items = append(items, homeListItem{title: "No snapshots for selected root"})
		}
	}
	return items
}

func homeBackupListRow(label string, snapshot app.Snapshot, width int) string {
	if width <= 0 {
		width = 1
	}
	started := compactSnapshotTime(snapshot.StartTime)
	size := formatBytes(snapshot.SizeBytes)
	startedWidth := 10
	sizeWidth := max(10, lipgloss.Width(size))
	labelWidth, _ := listRowColumnWidths(width, 1, startedWidth, sizeWidth)
	row := homeCell(label, labelWidth) + "  " + homeCell(started, startedWidth) + "  " + homeCell(size, sizeWidth)
	return homeTruncateStyled(row, width)
}

func listRowColumnWidths(width, minLeadingWidth int, trailingWidths ...int) (leadingWidth, trailingWidth int) {
	if width <= 0 {
		width = 1
	}
	for _, trailing := range trailingWidths {
		trailingWidth += max(1, trailing)
	}
	gaps := 2 * len(trailingWidths)
	trailingWidth += gaps
	leadingWidth = width - trailingWidth
	if leadingWidth < minLeadingWidth {
		leadingWidth = max(1, width-trailingWidth)
	}
	return leadingWidth, trailingWidth
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
	leftWidth, rightWidth, topHeight, bottomHeight := m.homeLayout(width)

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
	} else if m.repoDetailsLoading {
		lastBackup = "calculating"
	}
	rows := []string{
		homeDetailField("Root", homeShortPath(root.Path), width),
		homeDetailField("Last backup", lastBackup, width),
	}
	rows = append(rows, homeMetricRows(width,
		homeMetric{"Snapshots", m.rootSnapshotLabel(root)},
		homeMetric{"Size", formatBytes(root.SizeBytes)},
		homeMetric{"Protection", colorByStatus(root.Protection).Render(root.Protection)},
		homeMetric{"Peers", m.homePeerCountLabel()},
		homeMetric{"Receipts", fmt.Sprintf("%d", m.status.ReceiptCount)},
		homeMetric{"Audits", m.homeAuditLabel()},
	)...)
	if root.ProtectionNote != "" {
		rows = append(rows, homeDetailField("Health", root.ProtectionNote, width))
	}
	if root.LastFailure != "" {
		rows = append(rows, homeDetailField("Last error", warnStyle.Render(root.LastFailure), width))
	}
	for i, row := range rows {
		rows[i] = homeTruncateStyled(row, width)
	}
	return rows
}

func (m model) homeSelectedBackupRows(width int) []string {
	snapshot := m.selectedHomeSnapshot()
	if snapshot.ID == "" {
		if m.repoDetailsLoading {
			return []string{
				homeMutedStyle.Render("Calculating backups..."),
				homeMutedStyle.Render("Repository details are loading in the background."),
			}
		}
		return []string{
			homeMutedStyle.Render("No backup selected."),
			homeMutedStyle.Render("Select a root with snapshots first."),
		}
	}
	rows := []string{
		homeDetailField("Backup", m.homeSnapshotLabel(snapshot), width),
		homeDetailField("Snapshot ID", shortID(snapshot.ID), width),
		homeDetailField("Source", homeShortPath(snapshot.Source), width),
		homeDetailField("Started", firstNonEmpty(compactSnapshotTime(snapshot.StartTime), snapshot.StartTime), width),
	}
	rows = append(rows, homeMetricRows(width,
		homeMetric{"Files", fmt.Sprintf("%d", snapshot.Files)},
		homeMetric{"Dirs", fmt.Sprintf("%d", snapshot.Dirs)},
		homeMetric{"Stored", formatBytes(snapshot.SizeBytes)},
		homeMetric{"Logical", formatBytes(snapshot.LogicalSizeBytes)},
	)...)
	rows = append(rows, homeDetailField("Restore target", defaultRestoreTarget(snapshot.ID), width))
	for i, row := range rows {
		rows[i] = homeTruncateStyled(row, width)
	}
	return rows
}

func (m model) homeSystemSummaryRows(width int) []string {
	rows := []string{
		homeDetailField("Config", firstNonEmpty(m.summary.Path, m.configPath, "missing"), width),
	}
	rows = append(homeMetricRows(width,
		homeMetric{"Repository", m.homeRepoLabel()},
		homeMetric{"Storage", firstNonEmpty(m.status.StorageMode, "unknown")},
		homeMetric{"Daemon", m.homeDaemonLabel()},
		homeMetric{"Peers", m.homePeerCountLabel()},
		homeMetric{"Shards", fmt.Sprintf("%d data + %d parity", m.status.DataShards, m.status.ParityShards)},
		homeMetric{"Schedule", firstNonEmpty(m.summary.Schedule, m.status.Schedule, "unknown")},
		homeMetric{"Retention", firstNonEmpty(m.summary.Retention, m.status.Retention, "unknown")},
	), rows...)
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

func homeMetricRows(width int, metrics ...homeMetric) []string {
	if width <= 0 {
		width = 1
	}
	columns := homeMetricColumns(width)
	rows := []string{}
	cells := make([]homeMetric, 0, columns)
	for _, metric := range metrics {
		if metric.name == "" {
			continue
		}
		cells = append(cells, metric)
		if len(cells) == columns {
			rows = append(rows, homeMetricRow(width, cells))
			cells = cells[:0]
		}
	}
	if len(cells) > 0 {
		rows = append(rows, homeMetricRow(width, cells))
	}
	return rows
}

func homeMetricColumns(width int) int {
	switch {
	case width >= 96:
		return 3
	case width >= 52:
		return 2
	default:
		return 1
	}
}

func homeMetricRow(width int, cells []homeMetric) string {
	if len(cells) == 0 {
		return ""
	}
	gap := 2
	cellWidth := (width - gap*(len(cells)-1)) / len(cells)
	if cellWidth < 1 {
		cellWidth = 1
	}
	parts := make([]string, len(cells))
	for i, cell := range cells {
		parts[i] = homeCell(homeMetricCell(cell), cellWidth)
	}
	return homeTruncateStyled(strings.Join(parts, strings.Repeat(" ", gap)), width)
}

func homeMetricCell(metric homeMetric) string {
	return homeLabel(metric.name, homeMetricLabelWidth) + metric.value
}

func homeDetailField(name, value string, width int) string {
	return homeTruncateStyled(homeLabel(name, homeDetailLabelWidth)+value, width)
}

func homeLabel(name string, width int) string {
	label := name + ":"
	padding := width - lipgloss.Width(name)
	if padding < 0 {
		padding = 0
	}
	return homeMutedStyle.Render(label) + strings.Repeat(" ", padding+1)
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
			full:  [][]key.Binding{{homeKeys.Up, homeKeys.Down, homeKeys.Restore, homeKeys.Details}, {homeKeys.NextPane, homeKeys.PrevPane}, {homeKeys.Help, homeKeys.Quit}},
		}
	case homeFocusSystem:
		return homeHelpBindings{
			short: []key.Binding{homeKeys.NextPane, homeKeys.Start, homeKeys.Stop, homeKeys.Audit, homeKeys.Password, homeKeys.Details, homeKeys.Help, homeKeys.Quit},
			full:  [][]key.Binding{{homeKeys.NextPane, homeKeys.PrevPane, homeKeys.Details}, {homeKeys.Start, homeKeys.Stop, homeKeys.Audit, homeKeys.Password}, {homeKeys.Help, homeKeys.Quit}},
		}
	default:
		return homeHelpBindings{
			short: []key.Binding{homeKeys.Up, homeKeys.Down, homeKeys.Backup, homeKeys.Add, homeKeys.Remove, homeKeys.Details, homeKeys.NextPane, homeKeys.Help, homeKeys.Quit},
			full:  [][]key.Binding{{homeKeys.Up, homeKeys.Down, homeKeys.Details}, {homeKeys.Backup, homeKeys.Add, homeKeys.Remove}, {homeKeys.NextPane, homeKeys.PrevPane}, {homeKeys.Help, homeKeys.Quit}},
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
	if m.repoDetailsLoading {
		return "updating"
	}
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
	parts = append(parts, fmt.Sprintf("%s / %s", formatBytes(event.BytesDone), totalLabel(event.BytesTotal)))
	if event.BytesTotal > 0 {
		parts = append(parts, fmt.Sprintf("%d%%", percent))
	}
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
		"Snapshots: " + m.rootSnapshotLabel(root),
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
		"New storage: " + formatBytes(snapshot.SizeBytes),
		"Logical size: " + formatBytes(snapshot.LogicalSizeBytes),
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

func (m model) setupNotice() string {
	if m.configPath == "" {
		return "Setup needed: press i to initialize p2pbk before backing up."
	}
	if m.status.Repository == "not initialized" {
		return "Setup needed: press i to initialize the backup store, then add a folder or press b to back up."
	}
	if strings.Contains(strings.ToLower(m.status.RepositoryError), "password") {
		return "Backups are locked: press p to enter the p2pbk password for this session."
	}
	if needsBackupStoreInit(m.status.RepositoryError) {
		return "Setup needed: press i to initialize the backup store before backing up."
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
		return "Backup store is not initialized yet. Press i to initialize it before backing up."
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

func (m model) nextMaintenanceLabel() string {
	if m.repoDetailsLoading {
		return "calculating"
	}
	next := formatTime(m.maint.NextQuick)
	if next == "unknown" {
		next = formatTime(m.maint.NextFull)
	}
	return next
}

func (m model) rootSnapshotLabel(root app.RootStatus) string {
	if m.repoDetailsLoading && root.Snapshots == 0 {
		return "calculating"
	}
	return fmt.Sprintf("%d", root.Snapshots)
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
		status, err := app.CollectFastStatus(context.Background(), cfg, configPath, password)
		summary, summaryErr := app.SummarizeConfig(cfg, configPath)
		if err == nil {
			err = summaryErr
		}
		return statusMsg{
			status:  status,
			daemon:  app.CheckDaemonStatus(context.Background(), cfg, configPath),
			summary: summary,
			err:     err,
		}
	}
}

func fetchRepositoryDetails(cfg config.Config, configPath, password string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		snapshots, maint, err := app.LoadRepositoryDetails(ctx, cfg, configPath, password)
		return repoDetailsMsg{snapshots: snapshots, maint: maint, err: err}
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

func clipText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	var out strings.Builder
	for _, r := range value {
		if lipgloss.Width(out.String()+string(r)) > width {
			break
		}
		out.WriteRune(r)
	}
	return out.String()
}

func clipStyledText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return clipText(stripANSI(value), width)
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

func compactRootBackupTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "never" {
		return "never"
	}
	return clipText(compactSnapshotTime(value), 10)
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
