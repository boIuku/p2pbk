package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"p2pbk/internal/config"
)

type DaemonStatus struct {
	LogPath   string
	Running   bool
	LastLines []string
	Note      string
}

func CheckDaemonStatus(_ context.Context, cfg config.Config, configPath string) DaemonStatus {
	logPath := ""
	if configPath != "" {
		logPath = filepath.Join(filepath.Dir(configPath), "p2pbk.log")
	}
	status := DaemonStatus{
		LogPath: logPath,
		Note:    "daemon pid file not found",
	}
	if logPath == "" {
		return status
	}
	if pid, ok := readDaemonPID(daemonPIDPath(configPath)); ok {
		if processRunning(pid) {
			status.Running = true
			status.Note = "running"
		} else {
			status.Note = "stale daemon pid file"
		}
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return status
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	status.LastLines = lines
	if !status.Running && status.Note == "daemon pid file not found" {
		status.Note = "not running; showing recent daemon log"
	}
	_ = cfg
	return status
}
