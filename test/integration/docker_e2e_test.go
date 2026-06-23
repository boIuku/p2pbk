package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestDockerE2EBackupRestore(t *testing.T) {
	if os.Getenv("P2PBK_DOCKER_E2E") != "1" {
		t.Skip("set P2PBK_DOCKER_E2E=1 to run Docker integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	root := repoRoot(t)
	work := t.TempDir()
	project := dockerProjectName()
	composePath := filepath.Join(work, "compose.yaml")
	configDir := filepath.Join(work, "configs")
	fixtureDir := filepath.Join(work, "fixtures")
	restoreDir := filepath.Join(work, "restore")
	clientDir := filepath.Join(work, "client")
	for _, dir := range []string{configDir, filepath.Join(fixtureDir, "nested"), restoreDir, clientDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) returned error: %v", dir, err)
		}
	}

	fixtureText := "hello from docker p2pbk integration\n"
	if err := os.WriteFile(filepath.Join(fixtureDir, "nested", "file.txt"), []byte(fixtureText), 0o644); err != nil {
		t.Fatalf("WriteFile fixture returned error: %v", err)
	}
	writeCompose(t, composePath, root, work)
	writeConfig(t, filepath.Join(configDir, "bootstrap.json"), dockerNodeConfig("/data/bootstrap", nil, 0))

	docker := dockerCompose{t: t, project: project, composePath: composePath}
	if out, err := docker.run(ctx, "version"); err != nil {
		t.Fatalf("docker compose version failed: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if out, err := docker.run(cleanupCtx, "down", "-v", "--remove-orphans"); err != nil {
			t.Logf("docker compose down failed: %v\n%s", err, out)
		}
	})

	if out, err := docker.run(ctx, "build"); err != nil {
		t.Fatalf("docker compose build failed: %v\n%s", err, out)
	}
	if out, err := docker.run(ctx, "up", "-d", "bootstrap"); err != nil {
		t.Fatalf("docker compose up bootstrap failed: %v\n%s", err, out)
	}
	bootstrapID := docker.waitForLogMatch(ctx, "bootstrap", regexp.MustCompile(`p2pbk bootstrap peer:\s+(\S+)`))
	bootstrapAddr := fmt.Sprintf("/dns4/bootstrap/tcp/4001/p2p/%s", bootstrapID)

	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("node%d", i)
		writeConfig(t, filepath.Join(configDir, name+".json"), dockerNodeConfig("/data/"+name, []string{bootstrapAddr}, 10_000_000_000))
	}
	if out, err := docker.run(ctx, "up", "-d", "node1", "node2", "node3", "client"); err != nil {
		t.Fatalf("docker compose up nodes failed: %v\n%s", err, out)
	}

	var storagePeers []string
	for i := 1; i <= 3; i++ {
		service := fmt.Sprintf("node%d", i)
		peerID := docker.waitForLogMatch(ctx, service, regexp.MustCompile(`p2pbk peer:\s+(\S+)`))
		storagePeers = append(storagePeers, fmt.Sprintf("/dns4/%s/tcp/4001/p2p/%s", service, peerID))
		docker.waitForLogMatch(ctx, service, regexp.MustCompile(`connected bootstrap:`))
	}

	initArgs := []string{
		"exec", "-T", "client", "p2pbk",
		"--config", "/work/client/config.json",
		"init",
		"--data-dir", "/work/client/data",
		"--dir", "/work/fixtures",
		"--max-space", "10GB",
		"--offer-space", "0GB",
		"--schedule", "daily",
		"--passphrase", "test-pass",
		"--start=false",
	}
	for _, peer := range storagePeers {
		initArgs = append(initArgs, "--bootstrap-peer", peer)
	}
	if out, err := docker.run(ctx, initArgs...); err != nil {
		t.Fatalf("client init failed: %v\n%s", err, out)
	}

	env := []string{
		"exec", "-T",
		"-e", "P2PBK_REPOSITORY_PASSWORD=test-repo-pass",
		"-e", "P2PBK_IDENTITY_PASSPHRASE=test-pass",
		"client", "p2pbk", "--config", "/work/client/config.json",
	}
	if out, err := docker.run(ctx, append(env, "repo", "init")...); err != nil {
		t.Fatalf("repo init failed: %v\n%s", err, out)
	}
	if out, err := docker.run(ctx, append(env, "backup", "now")...); err != nil {
		t.Fatalf("backup now failed: %v\n%s", err, out)
	}
	if out, err := docker.run(ctx, append(env, "snapshot", "list")...); err != nil {
		t.Fatalf("snapshot list failed: %v\n%s", err, out)
	}
	if out, err := docker.run(ctx, append(env, "restore", "latest", "/work/restore")...); err != nil {
		t.Fatalf("restore failed: %v\n%s", err, out)
	}

	restored := findRestoredFile(t, restoreDir, "file.txt")
	actual, err := os.ReadFile(restored)
	if err != nil {
		t.Fatalf("ReadFile restored file returned error: %v", err)
	}
	if string(actual) != fixtureText {
		t.Fatalf("restored file content = %q, want %q", string(actual), fixtureText)
	}
}

type dockerCompose struct {
	t           *testing.T
	project     string
	composePath string
}

func (d dockerCompose) run(ctx context.Context, args ...string) (string, error) {
	d.t.Helper()

	fullArgs := append([]string{"compose", "-p", d.project, "-f", d.composePath}, args...)
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func (d dockerCompose) waitForLogMatch(ctx context.Context, service string, pattern *regexp.Regexp) string {
	d.t.Helper()

	deadline := time.Now().Add(45 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			d.dumpLogs(ctx)
			d.t.Fatalf("context ended while waiting for %s log %q: %v", service, pattern.String(), ctx.Err())
		default:
		}

		var err error
		logs, err = d.run(ctx, "logs", "--no-color", service)
		if err == nil {
			if matches := pattern.FindStringSubmatch(logs); len(matches) > 1 {
				return matches[1]
			} else if len(matches) == 1 {
				return matches[0]
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	d.dumpLogs(ctx)
	d.t.Fatalf("timed out waiting for %s log %q\nlast logs:\n%s", service, pattern.String(), logs)
	return ""
}

func (d dockerCompose) dumpLogs(ctx context.Context) {
	d.t.Helper()
	if out, err := d.run(ctx, "logs", "--no-color"); err != nil {
		d.t.Logf("docker compose logs failed: %v\n%s", err, out)
	} else {
		d.t.Logf("docker compose logs:\n%s", out)
	}
}

func writeCompose(t *testing.T, path, root, work string) {
	t.Helper()

	content := fmt.Sprintf(`services:
  bootstrap:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    command: ["--config", "/work/configs/bootstrap.json", "bootstrap"]
    volumes:
      - %s:/work
  node1:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    command: ["--config", "/work/configs/node1.json", "daemon"]
    volumes:
      - %s:/work
      - node1-data:/data
  node2:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    command: ["--config", "/work/configs/node2.json", "daemon"]
    volumes:
      - %s:/work
      - node2-data:/data
  node3:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    command: ["--config", "/work/configs/node3.json", "daemon"]
    volumes:
      - %s:/work
      - node3-data:/data
  client:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    entrypoint: ["sleep"]
    command: ["3600000"]
    volumes:
      - %s:/work

volumes:
  node1-data:
  node2-data:
  node3-data:
`, yamlQuote(filepath.ToSlash(root)), yamlQuote(filepath.ToSlash(work)),
		yamlQuote(filepath.ToSlash(root)), yamlQuote(filepath.ToSlash(work)),
		yamlQuote(filepath.ToSlash(root)), yamlQuote(filepath.ToSlash(work)),
		yamlQuote(filepath.ToSlash(root)), yamlQuote(filepath.ToSlash(work)),
		yamlQuote(filepath.ToSlash(root)), yamlQuote(filepath.ToSlash(work)))

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile compose returned error: %v", err)
	}
}

func dockerNodeConfig(dataDir string, bootstrapPeers []string, offerBytes int64) map[string]any {
	if bootstrapPeers == nil {
		bootstrapPeers = []string{}
	}
	return map[string]any{
		"node": map[string]any{
			"data_dir":     dataDir,
			"identity":     "identity.age",
			"display_name": "",
		},
		"storage": map[string]any{
			"store_dir":        "store",
			"cache_dir":        "cache",
			"cache_size_bytes": int64(1 << 30),
			"offer_bytes":      offerBytes,
		},
		"network": map[string]any{
			"listen_addrs": []string{
				"/ip4/0.0.0.0/tcp/4001",
			},
			"bootstrap_peers": bootstrapPeers,
			"protocol_id":     "/p2pbk/1.0.0",
			"min_peers":       0,
		},
		"backup": map[string]any{
			"data_shards":   2,
			"parity_shards": 1,
			"roots":         []string{},
			"max_bytes":     int64(10_000_000_000),
			"schedule":      "daily",
			"retention":     "30d",
			"initial_full":  true,
			"incremental":   true,
		},
		"trust": map[string]any{
			"challenge_interval":   "1h",
			"reputation_window":    "720h",
			"challenge_max_length": int64(65536),
			"identity_passphrase":  "",
		},
	}
}

func writeConfig(t *testing.T, path string, cfg map[string]any) {
	t.Helper()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent config returned error: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile config returned error: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("could not find repo root")
		}
		wd = parent
	}
}

func dockerProjectName() string {
	return "p2pbk-e2e-" + strings.ToLower(strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
}

func yamlQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func findRestoredFile(t *testing.T, root, name string) string {
	t.Helper()

	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != name {
			return err
		}
		found = path
		return filepath.SkipAll
	})
	if err != nil {
		t.Fatalf("WalkDir restore returned error: %v", err)
	}
	if found == "" {
		t.Fatalf("restored %s not found under %s", name, root)
	}
	return found
}
