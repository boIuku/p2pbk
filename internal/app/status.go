package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"p2pbk/internal/config"
	"p2pbk/internal/kopiaengine"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/scheduler"
)

type Snapshot struct {
	ID          string
	Source      string
	Description string
	StartTime   string
	Files       int
	Dirs        int
	SizeBytes   int64
}

type RootStatus struct {
	Path           string
	Kind           string
	Snapshots      int
	SizeBytes      int64
	LastSuccess    string
	LastFailure    string
	Protection     string
	ProtectionNote string
}

type PeerStatus struct {
	ID           string
	Addrs        []string
	Source       string
	Status       string
	Score        float64
	StoredShards int
	Failures     int
	LastSeen     time.Time
}

type Status struct {
	ConfigPath        string
	DataDir           string
	StoreDir          string
	CacheDir          string
	LocalDB           string
	LocalDBStatus     string
	StorageBytes      int64
	StorageMode       string
	Repository        string
	RepositoryPath    string
	RepositoryError   string
	Schedule          string
	Retention         string
	MaxProtectedBytes int64
	DataShards        int
	ParityShards      int
	Roots             []RootStatus
	Snapshots         []Snapshot
	Peers             []PeerStatus
	ReceiptCount      int
	AuditCount        int
	CollectedAt       time.Time
}

func CollectStatus(ctx context.Context, cfg config.Config, configPath, password string) (Status, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return Status{}, err
	}

	status := Status{
		ConfigPath:        configPath,
		DataDir:           paths.DataDir,
		StoreDir:          paths.StoreDir,
		CacheDir:          paths.CacheDir,
		LocalDB:           paths.LocalDB,
		LocalDBStatus:     "available",
		StorageBytes:      dirSize(paths.StoreDir),
		StorageMode:       "local only",
		Repository:        "not initialized",
		Schedule:          cfg.Backup.Schedule,
		Retention:         cfg.Backup.Retention,
		MaxProtectedBytes: cfg.Backup.MaxBytes,
		DataShards:        cfg.Backup.DataShards,
		ParityShards:      cfg.Backup.ParityShards,
		CollectedAt:       time.Now(),
	}

	engine := kopiaengine.New(configPath, cfg, password)
	status.RepositoryPath = engine.RepositoryConfigPath()
	if engine.RepositoryConnected() {
		status.Repository = "initialized"
		if password == "" {
			status.RepositoryError = "p2pbk password required to list backups"
		} else {
			snapshots, err := engine.ListSnapshots(ctx)
			if err != nil {
				status.RepositoryError = err.Error()
			} else {
				for _, snapshot := range snapshots {
					status.Snapshots = append(status.Snapshots, Snapshot{
						ID:          string(snapshot.ID),
						Source:      fmt.Sprint(snapshot.Source),
						Description: snapshot.Description,
						StartTime:   snapshot.StartTime,
						Files:       int(snapshot.Stats.TotalFileCount),
						Dirs:        int(snapshot.Stats.TotalDirectoryCount),
						SizeBytes:   snapshot.Stats.TotalFileSize,
					})
				}
			}
		}
	}

	var lastSuccess map[string]string
	var lastFailure map[string]string
	var healthyPeers int
	if db, err := localdb.Open(paths.LocalDB); err == nil {
		lastSuccess, lastFailure = backupHistory(db, cfg.Backup.Roots)
		if stats, err := db.ListPeerStats(); err == nil {
			for _, stat := range stats {
				if stat.Status == "healthy" {
					healthyPeers++
				}
				status.Peers = append(status.Peers, PeerStatus{
					ID:           stat.ID,
					Addrs:        stat.Addrs,
					Source:       stat.Source,
					Status:       stat.Status,
					Score:        stat.Score,
					StoredShards: stat.StoredShards,
					Failures:     stat.Failures,
					LastSeen:     stat.LastSeen,
				})
			}
		}
		if receipts, err := db.ListShardReceipts(); err == nil {
			status.ReceiptCount = len(receipts)
		}
		if audits, err := db.ListAuditHistory(""); err == nil {
			status.AuditCount = len(audits)
		}
		_ = db.Close()
	} else if localdb.IsOpenTimeout(err) {
		status.LocalDBStatus = "busy; daemon is using it"
	} else {
		status.LocalDBStatus = "unavailable: " + err.Error()
	}

	if len(cfg.Network.BootstrapPeers) > 0 || len(cfg.Network.PublicDiscoveryPeers) > 0 || len(status.Peers) > 0 {
		status.StorageMode = "distributed"
	}

	for _, root := range NormalizeRootList(cfg.Backup.Roots) {
		status.Roots = append(status.Roots, RootStatus{
			Path:           root,
			Kind:           pathKind(root),
			Snapshots:      countSnapshots(status.Snapshots, root),
			SizeBytes:      dirSize(root),
			LastSuccess:    lastSuccess[root],
			LastFailure:    lastFailure[root],
			Protection:     protection(status, root, healthyPeers),
			ProtectionNote: protectionNote(status, healthyPeers),
		})
	}

	return status, nil
}

func backupHistory(db *localdb.DB, roots []string) (map[string]string, map[string]string) {
	success := map[string]string{}
	failure := map[string]string{}
	for _, root := range roots {
		if value, err := db.GetSetting(scheduler.LastSuccessKey(root)); err == nil && len(value) > 0 {
			success[root] = string(value)
		}
		if value, err := db.GetSetting(scheduler.LastFailureKey(root)); err == nil && len(value) > 0 {
			failure[root] = string(value)
		}
	}
	return success, failure
}

func pathKind(path string) string {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err != nil {
		return "unknown"
	}
	if info.IsDir() {
		return "dir"
	}
	return "file"
}

func countSnapshots(snapshots []Snapshot, root string) int {
	clean := filepath.Clean(root)
	base := filepath.Base(clean)
	count := 0
	for _, snapshot := range snapshots {
		source := filepath.Clean(snapshot.Source)
		if strings.Contains(source, clean) || strings.Contains(source, base) {
			count++
		}
	}
	return count
}

func protection(status Status, root string, healthyPeers int) string {
	if pathKind(root) == "missing" {
		return "missing"
	}
	if countSnapshots(status.Snapshots, root) == 0 {
		return "unprotected"
	}
	if status.StorageMode != "distributed" {
		return "local only"
	}
	if status.ReceiptCount == 0 {
		return "offload unknown"
	}
	if healthyPeers >= status.DataShards {
		return "protected"
	}
	return "degraded"
}

func protectionNote(status Status, healthyPeers int) string {
	if status.StorageMode != "distributed" {
		return "no known storage peers"
	}
	if status.ReceiptCount == 0 {
		return "no shard receipts recorded"
	}
	return fmt.Sprintf("%d healthy peers, %d receipts", healthyPeers, status.ReceiptCount)
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
