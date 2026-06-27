package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"p2pbk/internal/config"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/secrets"
	"p2pbk/internal/trust/identity"
)

type InitRequest struct {
	DataDir              string
	BackupDir            string
	MaxSpace             string
	OfferSpace           string
	Schedule             string
	Retention            string
	ConfigPath           string
	Passphrase           string
	Start                bool
	DisplayName          string
	BootstrapPeers       []string
	PublicDiscoveryPeers []string
}

type InitResult struct {
	ConfigPath          string
	DataDir             string
	IdentityFingerprint string
	ProtectedRoots      int
	MaxProtectedBytes   int64
	NetworkStorageBytes int64
	OfferBytes          int64
	Started             bool
}

func DefaultInitRequest() InitRequest {
	defaultCfg := config.Default()
	return InitRequest{
		DataDir:    defaultCfg.Node.DataDir,
		MaxSpace:   "10GB",
		OfferSpace: "10GB",
		Schedule:   "daily",
		Retention:  "30d",
		Start:      false,
	}
}

func Initialize(req InitRequest) (InitResult, error) {
	if req.Passphrase == "" {
		return InitResult{}, errors.New("p2pbk password is required")
	}
	if req.DataDir == "" {
		req.DataDir = config.Default().Node.DataDir
	}
	if req.MaxSpace == "" {
		req.MaxSpace = "10GB"
	}
	if req.OfferSpace == "" {
		req.OfferSpace = "10GB"
	}
	if req.Schedule == "" {
		req.Schedule = "daily"
	}
	if req.Retention == "" {
		req.Retention = "30d"
	}

	cfg := config.Default()
	cfg.Node.DataDir = req.DataDir
	cfg.Node.DisplayName = req.DisplayName
	cfg.Storage.StoreDir = "store"
	cfg.Storage.CacheDir = "cache"
	cfg.Backup.Schedule = req.Schedule
	cfg.Backup.Retention = req.Retention
	cfg.Backup.InitialFull = true
	cfg.Backup.Incremental = true
	cfg.Network.BootstrapPeers = req.BootstrapPeers
	cfg.Network.PublicDiscoveryPeers = req.PublicDiscoveryPeers

	maxBytes, err := ParseByteSize(req.MaxSpace)
	if err != nil {
		return InitResult{}, fmt.Errorf("parse max space: %w", err)
	}
	offerBytes, err := ParseByteSize(req.OfferSpace)
	if err != nil {
		return InitResult{}, fmt.Errorf("parse offer space: %w", err)
	}
	cfg.Backup.MaxBytes = maxBytes
	cfg.Storage.OfferBytes = offerBytes
	if req.BackupDir != "" {
		cfg.Backup.Roots = []string{req.BackupDir}
	}

	paths, err := cfg.Paths()
	if err != nil {
		return InitResult{}, err
	}
	if err := os.MkdirAll(paths.StoreDir, 0o700); err != nil {
		return InitResult{}, fmt.Errorf("create store directory: %w", err)
	}
	if err := os.MkdirAll(paths.CacheDir, 0o700); err != nil {
		return InitResult{}, fmt.Errorf("create cache directory: %w", err)
	}

	id, err := identity.Create(paths.Identity, req.Passphrase)
	if err != nil {
		return InitResult{}, fmt.Errorf("create identity: %w", err)
	}

	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		return InitResult{}, err
	}
	if err := db.PutSetting("identity_fingerprint", []byte(id.Fingerprint())); err != nil {
		_ = db.Close()
		return InitResult{}, err
	}
	if err := db.Close(); err != nil {
		return InitResult{}, fmt.Errorf("close localdb: %w", err)
	}

	configPath := req.ConfigPath
	if configPath == "" {
		configPath = filepath.Join(paths.DataDir, "config.json")
	}
	if _, err := os.Stat(configPath); err == nil {
		return InitResult{}, fmt.Errorf("config already exists: %s", configPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return InitResult{}, err
	}
	if err := SaveConfig(cfg, configPath); err != nil {
		return InitResult{}, err
	}

	result := InitResult{
		ConfigPath:          configPath,
		DataDir:             paths.DataDir,
		IdentityFingerprint: id.Fingerprint(),
		ProtectedRoots:      len(cfg.Backup.Roots),
		MaxProtectedBytes:   cfg.Backup.MaxBytes,
		NetworkStorageBytes: ErasureStorageBytes(cfg.Backup.MaxBytes, cfg.Backup.DataShards, cfg.Backup.ParityShards),
		OfferBytes:          cfg.Storage.OfferBytes,
	}
	if req.Start {
		if err := StartDaemon(configPath, req.Passphrase); err != nil {
			return InitResult{}, err
		}
		result.Started = true
	}
	return result, nil
}

func StartDaemon(configPath, password string) error {
	if configPath == "" {
		return errors.New("config path is required")
	}
	pidPath := daemonPIDPath(configPath)
	if pid, ok := readDaemonPID(pidPath); ok && processRunning(pid) {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	logPath := filepath.Join(filepath.Dir(configPath), "p2pbk.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "--config", configPath, "daemon")
	cmd.Env = secrets.Env(password)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background p2pbk: %w", err)
	}
	pid := cmd.Process.Pid
	if err := writeDaemonPID(pidPath, pid); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	return cmd.Process.Release()
}

func StopDaemon(configPath string) error {
	if configPath == "" {
		return errors.New("config path is required")
	}
	pidPath := daemonPIDPath(configPath)
	pid, ok := readDaemonPID(pidPath)
	if !ok {
		return nil
	}
	if !processRunning(pid) {
		_ = os.Remove(pidPath)
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find daemon process %d: %w", pid, err)
	}
	if err := proc.Kill(); err != nil && processRunning(pid) {
		return fmt.Errorf("stop daemon process %d: %w", pid, err)
	}
	_ = os.Remove(pidPath)
	return nil
}

func daemonPIDPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "p2pbk.pid")
}

func writeDaemonPID(path string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create daemon pid directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		return fmt.Errorf("write daemon pid: %w", err)
	}
	return nil
}

func readDaemonPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func ErasureStorageBytes(sourceBytes int64, dataShards, parityShards int) int64 {
	if sourceBytes <= 0 || dataShards <= 0 {
		return 0
	}
	return sourceBytes * int64(dataShards+parityShards) / int64(dataShards)
}
