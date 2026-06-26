package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonPIDReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p2pbk.pid")
	if err := writeDaemonPID(path, 12345); err != nil {
		t.Fatalf("writeDaemonPID returned error: %v", err)
	}
	pid, ok := readDaemonPID(path)
	if !ok {
		t.Fatal("readDaemonPID did not read pid")
	}
	if pid != 12345 {
		t.Fatalf("pid = %d, want 12345", pid)
	}
}

func TestStopDaemonRemovesStalePID(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	pidPath := daemonPIDPath(configPath)
	if err := writeDaemonPID(pidPath, 99999999); err != nil {
		t.Fatalf("writeDaemonPID returned error: %v", err)
	}
	if err := StopDaemon(configPath); err != nil {
		t.Fatalf("StopDaemon returned error: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file still exists after stale stop: %v", err)
	}
}

func TestProcessRunningCurrentProcess(t *testing.T) {
	if !processRunning(os.Getpid()) {
		t.Fatal("current process should be running")
	}
}
