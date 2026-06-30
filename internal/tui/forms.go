package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"p2pbk/internal/app"
	"p2pbk/internal/config"
)

type configChangedMsg struct {
	cfg        config.Config
	configPath string
	message    string
	err        error
}

var browseableFields = map[string]map[int]bool{
	"add":     {0: true},
	"config":  {0: true, 2: true, 3: true},
	"init":    {0: true, 1: true},
	"restore": {1: true},
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.form = nil
		return m, nil
	case "ctrl+b":
		if browseableFields[m.form.kind][m.form.active] {
			m.picker = m.newPickerForActiveField()
		}
	case "ctrl+c":
		return m, tea.Quit
	case "up", "shift+tab":
		if m.form.active > 0 {
			m.form.active--
		}
	case "down", "tab":
		if m.form.active < len(m.form.fields)-1 {
			m.form.active++
		}
	case "enter":
		if m.form.active < len(m.form.fields)-1 {
			m.form.active++
			return m, nil
		}
		form := m.form
		m.form = nil
		return m, m.submitForm(form)
	case "backspace":
		value := m.form.fields[m.form.active].value
		if len(value) > 0 {
			m.form.fields[m.form.active].value = value[:len(value)-1]
		}
	default:
		if len(key.Runes) > 0 {
			m.form.fields[m.form.active].value += string(key.Runes)
		}
	}
	return m, nil
}

func (m model) formView() string {
	lines := []string{titleStyle.Render(m.form.title)}
	start, end := m.visibleFormRange()
	if start > 0 {
		lines = append(lines, mutedStyle.Render("..."))
	}
	for i := start; i < end; i++ {
		field := m.form.fields[i]
		prefix := " "
		if i == m.form.active {
			prefix = ">"
		}
		value := field.value
		if field.secret && value != "" {
			value = strings.Repeat("*", len(value))
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s", prefix, field.label, value))
	}
	if end < len(m.form.fields) {
		lines = append(lines, mutedStyle.Render("..."))
	}
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("enter next/submit  tab move  ctrl+b browse dir  esc cancel"))
	return "\n" + panelStyle.Render(strings.Join(lines, "\n")) + "\n"
}

func (m model) visibleFormRange() (int, int) {
	total := len(m.form.fields)
	if total == 0 {
		return 0, 0
	}
	maxFields := m.height - 7
	if maxFields < 4 {
		maxFields = 4
	}
	if maxFields >= total {
		return 0, total
	}
	start := m.form.active - maxFields/2
	if start < 0 {
		start = 0
	}
	end := start + maxFields
	if end > total {
		end = total
		start = max(0, end-maxFields)
	}
	return start, end
}

func addAssetForm() *formState {
	return &formState{
		kind:  "add",
		title: "Add Protected Folder",
		fields: []field{
			{label: "Path"},
		},
	}
}

func editConfigForm(cfg config.Config) *formState {
	return &formState{
		kind:  "config",
		title: "Edit Config",
		fields: []field{
			{label: "Data dir", value: cfg.Node.DataDir},
			{label: "Display name", value: cfg.Node.DisplayName},
			{label: "Store dir", value: cfg.Storage.StoreDir},
			{label: "Cache dir", value: cfg.Storage.CacheDir},
			{label: "Cache size", value: formatBytes(cfg.Storage.CacheSizeBytes)},
			{label: "Offer space", value: formatBytes(cfg.Storage.OfferBytes)},
			{label: "Max protected", value: formatBytes(cfg.Backup.MaxBytes)},
			{label: "Schedule", value: cfg.Backup.Schedule},
			{label: "Retention", value: cfg.Backup.Retention},
			{label: "Initial full", value: fmt.Sprintf("%t", cfg.Backup.InitialFull)},
			{label: "Incremental", value: fmt.Sprintf("%t", cfg.Backup.Incremental)},
			{label: "Listen addrs", value: strings.Join(cfg.Network.ListenAddrs, ",")},
			{label: "Bootstrap peers", value: strings.Join(cfg.Network.BootstrapPeers, ",")},
			{label: "Public discovery peers", value: strings.Join(cfg.Network.PublicDiscoveryPeers, ",")},
			{label: "Protocol ID", value: cfg.Network.ProtocolID},
			{label: "Min peers", value: fmt.Sprintf("%d", cfg.Network.MinPeers)},
			{label: "Discovery", value: cfg.Network.Discovery},
			{label: "Discovery service", value: cfg.Network.LocalDiscoveryService},
			{label: "Remote namespace", value: cfg.Network.RemoteDiscoveryNamespace},
			{label: "Remote interval", value: cfg.Network.RemoteDiscoveryInterval},
			{label: "Data shards", value: fmt.Sprintf("%d", cfg.Backup.DataShards)},
			{label: "Parity shards", value: fmt.Sprintf("%d", cfg.Backup.ParityShards)},
			{label: "Challenge interval", value: cfg.Trust.ChallengeInterval},
			{label: "Reputation window", value: cfg.Trust.ReputationWindow},
			{label: "Challenge max", value: fmt.Sprintf("%d", cfg.Trust.ChallengeMaxLength)},
		},
	}
}

func restoreForm(snapshotID string) *formState {
	return &formState{
		kind:  "restore",
		title: "Restore Backup",
		fields: []field{
			{label: "Backup", value: snapshotID},
			{label: "Target", value: defaultRestoreTarget(snapshotID)},
		},
	}
}

func initForm(configPath string) *formState {
	req := app.DefaultInitRequest()
	return &formState{
		kind:  "init",
		title: "Initialize p2pbk",
		fields: []field{
			{label: "Data dir", value: req.DataDir},
			{label: "Protected dir"},
			{label: "Config path", value: configPath},
			{label: "Passphrase", secret: true},
			{label: "Max space", value: req.MaxSpace},
			{label: "Offer space", value: req.OfferSpace},
			{label: "Schedule", value: req.Schedule},
			{label: "Retention", value: req.Retention},
			{label: "Display name"},
			{label: "Bootstrap peers"},
			{label: "Public discovery peers"},
			{label: "Start service", value: "no"},
		},
	}
}

func resetForm() *formState {
	return &formState{
		kind:  "reset",
		title: "Reset Local State",
		fields: []field{
			{label: "Type RESET"},
		},
	}
}

func removeFolderForm(path string) *formState {
	return &formState{
		kind:  "remove",
		title: "Remove Protected Folder",
		fields: []field{
			{label: "Path", value: path},
			{label: "Type REMOVE"},
		},
	}
}

func passwordForm() *formState {
	return &formState{
		kind:  "password",
		title: "p2pbk Password",
		fields: []field{
			{label: "Password", secret: true},
		},
	}
}

func (m *model) submitForm(form *formState) tea.Cmd {
	switch form.kind {
	case "password":
		m.password = form.fields[0].value
		if m.password == "" {
			return func() tea.Msg {
				return actionMsg{err: fmt.Errorf("p2pbk password is required")}
			}
		}
		m.repoDetailsRequested = true
		pending := m.pending
		m.pending = nil
		if pending == nil {
			return func() tea.Msg {
				return actionMsg{message: "p2pbk password saved for this session"}
			}
		}
		switch pending.kind {
		case "backup":
			return m.startBackup(pending.root)
		case "restore":
			return m.startRestore(pending.snapshot, pending.target)
		default:
			return func() tea.Msg {
				return actionMsg{message: "p2pbk password saved for this session"}
			}
		}
	case "add":
		paths := splitPathList(form.fields[0].value)
		return func() tea.Msg {
			cfg := m.cfg
			for _, path := range paths {
				var err error
				cfg, err = app.AddAsset(cfg, m.configPath, path)
				if err != nil {
					return configChangedMsg{cfg: cfg, configPath: m.configPath, err: err}
				}
			}
			return configChangedMsg{cfg: cfg, configPath: m.configPath, message: fmt.Sprintf("added %d protected folder(s)", len(paths))}
		}
	case "config":
		return func() tea.Msg {
			cfg := m.cfg
			err := applyConfigForm(&cfg, form)
			if err == nil {
				err = app.SaveConfig(cfg, m.configPath)
			}
			return configChangedMsg{cfg: cfg, configPath: m.configPath, message: "updated config", err: err}
		}
	case "init":
		return func() tea.Msg {
			start := strings.EqualFold(form.fields[11].value, "yes") || strings.EqualFold(form.fields[11].value, "y")
			protectedDirs := splitPathList(form.fields[1].value)
			backupDir := ""
			if len(protectedDirs) > 0 {
				backupDir = protectedDirs[0]
			}
			req := app.InitRequest{
				DataDir:              form.fields[0].value,
				BackupDir:            backupDir,
				ConfigPath:           form.fields[2].value,
				Passphrase:           form.fields[3].value,
				MaxSpace:             form.fields[4].value,
				OfferSpace:           form.fields[5].value,
				Schedule:             form.fields[6].value,
				Retention:            form.fields[7].value,
				DisplayName:          form.fields[8].value,
				BootstrapPeers:       splitCSV(form.fields[9].value),
				PublicDiscoveryPeers: splitCSV(form.fields[10].value),
				Start:                start,
			}
			result, err := app.Initialize(req)
			if err != nil {
				return configChangedMsg{err: err}
			}
			cfg, err := config.Load(result.ConfigPath)
			if err == nil && len(protectedDirs) > 1 {
				for _, path := range protectedDirs[1:] {
					cfg, err = app.AddAsset(cfg, result.ConfigPath, path)
					if err != nil {
						break
					}
				}
			}
			return configChangedMsg{
				cfg:        cfg,
				configPath: result.ConfigPath,
				message:    fmt.Sprintf("initialized %s with %d protected folder(s)", result.DataDir, len(protectedDirs)),
				err:        err,
			}
		}
	case "restore":
		snapshotID := form.fields[0].value
		target := form.fields[1].value
		if m.password == "" {
			m.pending = &pendingAction{kind: "restore", snapshot: snapshotID, target: target}
			m.form = passwordForm()
			return nil
		}
		return m.startRestore(snapshotID, target)
	case "remove":
		path := form.fields[0].value
		confirmation := form.fields[1].value
		return func() tea.Msg {
			if confirmation != "REMOVE" {
				return configChangedMsg{cfg: m.cfg, configPath: m.configPath, err: fmt.Errorf("remove cancelled: confirmation did not match REMOVE")}
			}
			cfg, err := app.RemoveAsset(m.cfg, m.configPath, path)
			return configChangedMsg{cfg: cfg, configPath: m.configPath, message: "removed protected folder: " + path, err: err}
		}
	case "reset":
		confirmation := form.fields[0].value
		return func() tea.Msg {
			if confirmation != "RESET" {
				return actionMsg{err: fmt.Errorf("reset cancelled: confirmation did not match RESET")}
			}
			result, err := app.ResetLocalState(app.ResetRequest{
				Config:     m.cfg,
				ConfigPath: m.configPath,
				Yes:        true,
			})
			if err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{message: "removed: " + strings.Join(result.Removed, ", ")}
		}
	default:
		return nil
	}
}

func (m model) newPickerForActiveField() *dirPickerState {
	field := m.form.fields[m.form.active]
	start := firstPathValue(field.value)
	if start == "" {
		start, _ = os.UserHomeDir()
	}
	multiple := m.form.kind == "add" || (m.form.kind == "init" && m.form.active == 1)
	picker := newDirPicker("Select "+field.label, start, m.form.active, multiple)
	picker.roots = m.cfg.Backup.Roots
	picker.resize(m.width, m.height)
	return picker
}

func firstPathValue(value string) string {
	paths := splitPathList(value)
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func splitPathList(value string) []string {
	parts := strings.Split(value, ";")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, filepath.Clean(part))
	}
	return out
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func applyConfigForm(cfg *config.Config, form *formState) error {
	cacheSize, err := app.ParseByteSize(form.fields[4].value)
	if err != nil {
		return fmt.Errorf("cache size: %w", err)
	}
	offerSpace, err := app.ParseByteSize(form.fields[5].value)
	if err != nil {
		return fmt.Errorf("offer space: %w", err)
	}
	maxProtected, err := app.ParseByteSize(form.fields[6].value)
	if err != nil {
		return fmt.Errorf("max protected: %w", err)
	}
	initialFull, err := parseBoolField("initial full", form.fields[9].value)
	if err != nil {
		return err
	}
	incremental, err := parseBoolField("incremental", form.fields[10].value)
	if err != nil {
		return err
	}
	minPeers, err := parseIntField("min peers", form.fields[15].value)
	if err != nil {
		return err
	}
	dataShards, err := parseIntField("data shards", form.fields[20].value)
	if err != nil {
		return err
	}
	parityShards, err := parseIntField("parity shards", form.fields[21].value)
	if err != nil {
		return err
	}
	challengeMax, err := strconv.ParseInt(strings.TrimSpace(form.fields[24].value), 10, 64)
	if err != nil {
		return fmt.Errorf("challenge max: %w", err)
	}

	cfg.Node.DataDir = form.fields[0].value
	cfg.Node.DisplayName = form.fields[1].value
	cfg.Storage.StoreDir = form.fields[2].value
	cfg.Storage.CacheDir = form.fields[3].value
	cfg.Storage.CacheSizeBytes = cacheSize
	cfg.Storage.OfferBytes = offerSpace
	cfg.Backup.MaxBytes = maxProtected
	cfg.Backup.Schedule = form.fields[7].value
	cfg.Backup.Retention = form.fields[8].value
	cfg.Backup.InitialFull = initialFull
	cfg.Backup.Incremental = incremental
	cfg.Network.ListenAddrs = splitCSV(form.fields[11].value)
	cfg.Network.BootstrapPeers = splitCSV(form.fields[12].value)
	cfg.Network.PublicDiscoveryPeers = splitCSV(form.fields[13].value)
	cfg.Network.ProtocolID = form.fields[14].value
	cfg.Network.MinPeers = minPeers
	cfg.Network.Discovery = form.fields[16].value
	cfg.Network.LocalDiscoveryService = form.fields[17].value
	cfg.Network.RemoteDiscoveryNamespace = form.fields[18].value
	cfg.Network.RemoteDiscoveryInterval = form.fields[19].value
	cfg.Backup.DataShards = dataShards
	cfg.Backup.ParityShards = parityShards
	cfg.Trust.ChallengeInterval = form.fields[22].value
	cfg.Trust.ReputationWindow = form.fields[23].value
	cfg.Trust.ChallengeMaxLength = challengeMax
	return cfg.Validate()
}

func parseBoolField(name, value string) (bool, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "true", "yes", "y", "1", "on":
		return true, nil
	case "false", "no", "n", "0", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func parseIntField(name, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n, nil
}
