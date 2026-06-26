package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"p2pbk/internal/app"
	"p2pbk/internal/config"
)

const (
	tabDashboard = iota
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

var tabs = []string{"Dashboard", "Folders", "Backups", "Settings"}

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

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63")).Padding(0, 1)
	panelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	cardStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	cardTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	cardRule     = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
	badgeStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	activeStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
)

func Run(cfg config.Config, configPath, password string) error {
	tab := tabDashboard
	if configPath == "" {
		tab = tabSettings
	}
	bar := progress.New(progress.WithDefaultGradient())
	m := model{cfg: cfg, configPath: configPath, password: password, tab: tab, loading: true, progress: bar}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchStatus(m.cfg, m.configPath, m.password), tick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
		if m.picker != nil {
			m.picker.resize(size.Width, size.Height)
		}
		return m, nil
	}
	if m.picker != nil {
		return m.updateDirPicker(msg)
	}
	if m.form != nil {
		return m.updateForm(msg)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
			if m.tab == tabFolders {
				m.moveFolderSelection(-1)
			} else if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.tab == tabFolders {
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
			if m.tab == tabDashboard && m.op == nil && len(m.status.Snapshots) > 0 {
				m.form = restoreForm(m.latestBackup().ID)
				return m, nil
			}
			return m, fetchStatus(m.cfg, m.configPath, m.password)
		case "b":
			if (m.tab == tabDashboard || m.tab == tabFolders) && m.op == nil && len(m.status.Roots) > 0 {
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
			if (m.tab == tabDashboard || (m.tab == tabSettings && m.settingsTab == settingsService)) && m.op == nil {
				return m, startServiceCmd(m.configPath, m.password)
			}
		case "S":
			if (m.tab == tabDashboard || (m.tab == tabSettings && m.settingsTab == settingsService)) && m.op == nil {
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
		return "\n  " + mutedStyle.Render("loading p2pbk...") + "\n"
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
	case tabFolders:
		body.WriteString(m.foldersView())
	case tabBackups:
		body.WriteString(m.backupsView())
	case tabSettings:
		body.WriteString(m.settingsView())
	default:
		body.WriteString(m.dashboardView())
	}
	if m.op != nil && m.tab != tabDashboard {
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
	body.WriteString(m.helpView())
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
		{"Local discovery", fmt.Sprintf("%t", m.summary.LocalDiscovery)},
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
		return "Backups are locked: press p to enter the backup store password for this session."
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
		return "Backup store password is required. Press p to enter it for this session."
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
	style := badgeStyle.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("244"))
	switch value {
	case "protected", "running", "initialized":
		style = badgeStyle.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("42"))
	case "local only", "offload unknown", "degraded", "stopped or unknown", "no folders":
		style = badgeStyle.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("214"))
	case "missing", "unprotected", "not initialized":
		style = badgeStyle.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("203"))
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
		(m.tab == tabDashboard && len(m.status.Snapshots) > 0)
	if !rRestores {
		parts = append(parts, "r refresh")
	}
	if m.maxSelected() > 0 {
		parts = append(parts, "up/down select")
	}
	switch m.tab {
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
		return lipgloss.NewStyle()
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
		BorderForeground(lipgloss.Color("63")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("86"))
	if selectable {
		styles.Selected = styles.Selected.
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("63")).
			Bold(false)
	} else {
		styles.Selected = lipgloss.NewStyle()
	}
	t.SetStyles(styles)
	return panelStyle.Render(t.View())
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
