package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type dirPickerState struct {
	title       string
	cwd         string
	entries     []dirEntry
	marked      map[string]struct{}
	targetField int
	multiple    bool
	err         error
	table       table.Model
	tablePaths  []string
	input       textinput.Model
	width       int
	height      int
	roots       []string
}

type dirEntry struct {
	name string
	path string
}

func newDirPicker(title string, start string, targetField int, multiple bool) *dirPickerState {
	if start == "" {
		start, _ = os.UserHomeDir()
	}
	input := textinput.New()
	input.Placeholder = "Paste or browse path"
	input.SetValue(filepath.Clean(start))
	input.Focus()
	picker := &dirPickerState{
		title:       title,
		cwd:         filepath.Clean(start),
		marked:      map[string]struct{}{},
		targetField: targetField,
		multiple:    multiple,
		input:       input,
	}
	picker.table = table.New(table.WithFocused(true))
	picker.load()
	return picker
}

func (p *dirPickerState) load() {
	p.err = nil
	p.entries = nil
	if p.cwd == "" || p.cwd == "drives" {
		p.cwd = "drives"
		p.entries = driveEntries()
		p.refreshTable()
		return
	}
	entries, err := os.ReadDir(p.cwd)
	if err != nil {
		p.err = err
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		p.entries = append(p.entries, dirEntry{name: name, path: filepath.Join(p.cwd, name)})
	}
	sort.Slice(p.entries, func(i, j int) bool {
		return strings.ToLower(p.entries[i].name) < strings.ToLower(p.entries[j].name)
	})
	p.refreshTable()
}

func driveEntries() []dirEntry {
	if runtime.GOOS != "windows" {
		return []dirEntry{{name: string(os.PathSeparator), path: string(os.PathSeparator)}}
	}
	out := []dirEntry{}
	for letter := 'A'; letter <= 'Z'; letter++ {
		root := string(letter) + `:\`
		if _, err := os.Stat(root); err == nil {
			out = append(out, dirEntry{name: root, path: root})
		}
	}
	return out
}

func (p *dirPickerState) currentPath() string {
	cursor := p.table.Cursor()
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(p.tablePaths) {
		return ""
	}
	return p.tablePaths[cursor]
}

func (p *dirPickerState) toggle(path string) {
	clean := filepath.Clean(path)
	if _, ok := p.marked[clean]; ok {
		delete(p.marked, clean)
		return
	}
	if !p.multiple {
		p.marked = map[string]struct{}{}
	}
	p.marked[clean] = struct{}{}
}

func (p *dirPickerState) values() []string {
	values := make([]string, 0, len(p.marked))
	for value := range p.marked {
		values = append(values, value)
	}
	sort.Strings(values)
	if len(values) == 0 && p.currentPath() != "" {
		values = append(values, filepath.Clean(p.currentPath()))
		return values
	}
	if len(values) == 0 && strings.TrimSpace(p.input.Value()) != "" {
		values = append(values, filepath.Clean(strings.TrimSpace(p.input.Value())))
		return values
	}
	return values
}

func (p *dirPickerState) resize(width, height int) {
	p.width = width
	p.height = height
	p.input.Width = max(20, width-12)
	p.refreshTable()
}

func (p *dirPickerState) refreshTable() {
	rows := []table.Row{}
	paths := []string{}
	if p.err != nil {
		rows = append(rows, table.Row{"", "error", p.err.Error()})
		paths = append(paths, "")
	}
	nameWidth, statusWidth, tableHeight := p.layout()
	for _, entry := range p.entries {
		rows = append(rows, table.Row{marker(p.marked, entry.path), middleTruncate(entry.name, nameWidth), statusForPath(entry.path, p.roots)})
		paths = append(paths, entry.path)
	}
	if len(p.entries) == 0 && p.err == nil {
		rows = append(rows, table.Row{"", "No child directories", "type or paste a path above"})
		paths = append(paths, "")
	}
	p.tablePaths = paths
	p.table.SetColumns([]table.Column{
		{Title: "Pick", Width: 4},
		{Title: "Directory", Width: nameWidth},
		{Title: "Status", Width: statusWidth},
	})
	p.table.SetRows(rows)
	p.table.SetHeight(tableHeight)
	p.table.Focus()
	if p.table.Cursor() >= len(rows) {
		p.table.SetCursor(max(0, len(rows)-1))
	}
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("63")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("86"))
	styles.Selected = styles.Selected.
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("63")).
		Bold(false)
	p.table.SetStyles(styles)
}

func (p *dirPickerState) layout() (int, int, int) {
	width := p.width
	if width <= 0 {
		width = 100
	}
	height := p.height
	if height <= 0 {
		height = 28
	}
	contentWidth := max(width-10, 44)
	statusWidth := 20
	if contentWidth < 70 {
		statusWidth = 16
	}
	nameWidth := max(contentWidth-statusWidth-8, 20)
	tableHeight := max(height-9, 5)

	return nameWidth, statusWidth, tableHeight
}

func (m model) updateDirPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	picker := m.picker
	switch key.String() {
	case "esc":
		m.picker = nil
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		picker.table, _ = updatePickerTable(picker.table, key)
		picker.input.SetValue(picker.currentPath())
	case "down", "j":
		picker.table, _ = updatePickerTable(picker.table, key)
		picker.input.SetValue(picker.currentPath())
	case "enter", "right", "l":
		current := picker.currentPath()
		if current != "" && current != picker.cwd {
			picker.cwd = current
			picker.table.SetCursor(0)
			picker.load()
			picker.input.SetValue(picker.currentPath())
		}
	case "backspace", "left", "h":
		if picker.cwd == "drives" {
			return m, nil
		}
		parent := filepath.Dir(picker.cwd)
		if parent == picker.cwd || parent == "." {
			picker.cwd = "drives"
		} else {
			picker.cwd = parent
		}
		picker.table.SetCursor(0)
		picker.load()
		picker.input.SetValue(picker.currentPath())
	case " ":
		current := picker.currentPath()
		if current != "" {
			picker.toggle(current)
			picker.refreshTable()
			picker.input.SetValue(current)
		}
	case "ctrl+u":
		picker.input.SetValue("")
	case "ctrl+s":
		return m, m.applyPickerSelection()
	case "y":
		return m, m.applyPickerSelection()
	default:
		if len(key.Runes) > 0 {
			picker.input, _ = picker.input.Update(key)
		}
	}
	return m, nil
}

func updatePickerTable(t table.Model, key tea.KeyMsg) (table.Model, tea.Cmd) {
	switch key.String() {
	case "k":
		key = tea.KeyMsg{Type: tea.KeyUp}
	case "j":
		key = tea.KeyMsg{Type: tea.KeyDown}
	}
	updated, cmd := t.Update(key)
	return updated, cmd
}

func (m *model) applyPickerSelection() tea.Cmd {
	if m.form == nil || m.picker == nil {
		return nil
	}
	values := m.picker.values()
	m.form.fields[m.picker.targetField].value = strings.Join(values, ";")
	m.picker = nil
	if m.form.kind == "add" {
		form := m.form
		m.form = nil
		return m.submitForm(form)
	}
	return nil
}

func (m model) dirPickerView() string {
	picker := m.picker
	picker.refreshTable()
	selected := picker.currentPath()
	if selected == "" {
		selected = picker.cwd
	}
	selected = middleTruncate(selected, max(20, picker.width-14))
	body := panelStyle.Render(picker.table.View())
	return "\n" + titleStyle.Render(picker.title) + " " + mutedStyle.Render(middleTruncate(picker.cwd, max(20, picker.width-24))) + "\n" + picker.input.View() + "\n" + body + "\n" + mutedStyle.Render("Highlighted: "+selected) + "\n" + mutedStyle.Render("space mark highlighted  y accept  enter open  backspace up  ctrl+u clear  esc cancel") + "\n"
}

func marker(marked map[string]struct{}, path string) string {
	if _, ok := marked[filepath.Clean(path)]; ok {
		return "[x]"
	}
	return "[ ]"
}

func statusForPath(path string, roots []string) string {
	clean, err := filepath.Abs(path)
	if err != nil {
		clean = filepath.Clean(path)
	}
	clean = filepath.Clean(clean)
	for _, root := range roots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			rootAbs = filepath.Clean(root)
		}
		rootAbs = filepath.Clean(rootAbs)
		if strings.EqualFold(clean, rootAbs) {
			return successStyle.Render("protected")
		}
		if isChildOf(clean, rootAbs) {
			return warnStyle.Render("inside protected")
		}
		if isChildOf(rootAbs, clean) {
			return warnStyle.Render("contains protected")
		}
	}
	return mutedStyle.Render("not protected")
}

func isChildOf(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
