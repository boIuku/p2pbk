package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	fixturePath := filepath.Join(fixtureDir, "nested", "file.txt")
	if err := os.WriteFile(fixturePath, []byte(fixtureText), 0o644); err != nil {
		t.Fatalf("WriteFile fixture returned error: %v", err)
	}
	t.Logf("fixture: %s (%d bytes)", fixturePath, len(fixtureText))
	t.Logf("restore target: %s", restoreDir)
	writeCompose(t, composePath, root, work)
	writeConfig(t, filepath.Join(configDir, "bootstrap.json"), dockerNodeConfig("/data/bootstrap", nil, 0))

	docker := dockerCompose{t: t, project: project, composePath: composePath}
	if out, err := docker.run(ctx, "version"); err != nil {
		t.Fatalf("docker compose version failed: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if os.Getenv("P2PBK_DOCKER_E2E_LOGS") == "1" {
			docker.dumpLogs(cleanupCtx)
		}
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
	t.Logf("bootstrap peer: %s", bootstrapAddr)

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
		peerAddr := fmt.Sprintf("/dns4/%s/tcp/4001/p2p/%s", service, peerID)
		storagePeers = append(storagePeers, peerAddr)
		t.Logf("storage peer %s: %s", service, peerAddr)
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
		"--passphrase", "test-repo-pass",
		"--start=false",
	}
	for _, peer := range storagePeers {
		initArgs = append(initArgs, "--bootstrap-peer", peer)
	}
	if out, err := docker.run(ctx, initArgs...); err != nil {
		t.Fatalf("client init failed: %v\n%s", err, out)
	} else {
		t.Logf("client init output:\n%s", strings.TrimSpace(out))
	}

	env := []string{
		"exec", "-T",
		"-e", "P2PBK_REPOSITORY_PASSWORD=test-repo-pass",
		"client", "p2pbk", "--config", "/work/client/config.json",
	}
	if out, err := docker.run(ctx, append(env, "repo", "init")...); err != nil {
		t.Fatalf("repo init failed: %v\n%s", err, out)
	} else {
		t.Logf("repo init output:\n%s", strings.TrimSpace(out))
	}
	if out, err := docker.run(ctx, append(env, "backup", "now")...); err != nil {
		t.Fatalf("backup now failed: %v\n%s", err, out)
	} else {
		t.Logf("backup output:\n%s", strings.TrimSpace(out))
	}
	if out, err := docker.run(ctx, append(env, "snapshot", "list")...); err != nil {
		t.Fatalf("snapshot list failed: %v\n%s", err, out)
	} else {
		t.Logf("snapshot list output:\n%s", strings.TrimSpace(out))
	}
	if out, err := docker.run(ctx, append(env, "restore", "latest", "/work/restore")...); err != nil {
		t.Fatalf("restore failed: %v\n%s", err, out)
	} else {
		t.Logf("restore output:\n%s", strings.TrimSpace(out))
	}

	restored := findRestoredFile(t, restoreDir, "file.txt")
	actual, err := os.ReadFile(restored)
	if err != nil {
		t.Fatalf("ReadFile restored file returned error: %v", err)
	}
	if string(actual) != fixtureText {
		t.Fatalf("restored file content = %q, want %q", string(actual), fixtureText)
	}
	t.Logf("restored file: %s (%d bytes)", restored, len(actual))
	t.Logf("byte comparison: restored content matches original fixture")
	for i := 1; i <= 3; i++ {
		service := fmt.Sprintf("node%d", i)
		if out, err := docker.run(ctx, "exec", "-T", service, "p2pbk", "--config", fmt.Sprintf("/work/configs/%s.json", service), "status"); err != nil {
			t.Logf("%s status failed: %v\n%s", service, err, out)
		} else {
			t.Logf("%s status after backup:\n%s", service, strings.TrimSpace(out))
		}
	}
	t.Logf("summary: backed up %d fixture byte(s), restored %d byte(s), used %d storage peer(s)", len(fixtureText), len(actual), len(storagePeers))
}

func TestDockerE2ERestoreAfterMaxNodeLoss(t *testing.T) {
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
	restoreDir := filepath.Join(work, "restore-degraded")
	clientDir := filepath.Join(work, "client")
	for _, dir := range []string{configDir, filepath.Join(fixtureDir, "resilience"), restoreDir, clientDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) returned error: %v", dir, err)
		}
	}

	fixtureBytes := deterministicBytes(512 * 1024)
	fixtureHash := sha256.Sum256(fixtureBytes)
	fixturePath := filepath.Join(fixtureDir, "resilience", "degraded-restore.bin")
	if err := os.WriteFile(fixturePath, fixtureBytes, 0o644); err != nil {
		t.Fatalf("WriteFile fixture returned error: %v", err)
	}
	t.Logf("fixture: %s (%d bytes, sha256=%x)", fixturePath, len(fixtureBytes), fixtureHash)
	t.Logf("restore target after node loss: %s", restoreDir)
	writeCompose(t, composePath, root, work)
	writeConfig(t, filepath.Join(configDir, "bootstrap.json"), dockerNodeConfig("/data/bootstrap", nil, 0))

	docker := dockerCompose{t: t, project: project, composePath: composePath}
	if out, err := docker.run(ctx, "version"); err != nil {
		t.Fatalf("docker compose version failed: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if os.Getenv("P2PBK_DOCKER_E2E_LOGS") == "1" {
			docker.dumpLogs(cleanupCtx)
		}
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
	t.Logf("bootstrap peer: %s", bootstrapAddr)

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
		peerAddr := fmt.Sprintf("/dns4/%s/tcp/4001/p2p/%s", service, peerID)
		storagePeers = append(storagePeers, peerAddr)
		t.Logf("storage peer %s: %s", service, peerAddr)
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
		"--passphrase", "test-repo-pass",
		"--start=false",
	}
	for _, peer := range storagePeers {
		initArgs = append(initArgs, "--bootstrap-peer", peer)
	}
	if out, err := docker.run(ctx, initArgs...); err != nil {
		t.Fatalf("client init failed: %v\n%s", err, out)
	} else {
		t.Logf("client init output:\n%s", strings.TrimSpace(out))
	}

	env := []string{
		"exec", "-T",
		"-e", "P2PBK_REPOSITORY_PASSWORD=test-repo-pass",
		"client", "p2pbk", "--config", "/work/client/config.json",
	}
	if out, err := docker.run(ctx, append(env, "repo", "init")...); err != nil {
		t.Fatalf("repo init failed: %v\n%s", err, out)
	} else {
		t.Logf("repo init output:\n%s", strings.TrimSpace(out))
	}
	if out, err := docker.run(ctx, append(env, "backup", "now")...); err != nil {
		t.Fatalf("backup now failed: %v\n%s", err, out)
	} else {
		t.Logf("backup output:\n%s", strings.TrimSpace(out))
	}
	for i := 1; i <= 3; i++ {
		service := fmt.Sprintf("node%d", i)
		if out, err := docker.run(ctx, "exec", "-T", service, "p2pbk", "--config", fmt.Sprintf("/work/configs/%s.json", service), "status"); err != nil {
			t.Logf("%s status before loss failed: %v\n%s", service, err, out)
		} else {
			t.Logf("%s status before loss:\n%s", service, strings.TrimSpace(out))
		}
	}

	lostNode := "node3"
	lostPeers := 1
	dataShards := 2
	parityShards := 1
	t.Logf("stopping %s: maximum tolerated node loss for %d+%d erasure policy is %d", lostNode, dataShards, parityShards, parityShards)
	if out, err := docker.run(ctx, "stop", lostNode); err != nil {
		t.Fatalf("stop %s failed: %v\n%s", lostNode, err, out)
	}
	setClientBootstrapPeers(t, filepath.Join(clientDir, "config.json"), storagePeers[:2])
	t.Logf("client restore config now uses surviving peers: %v", storagePeers[:2])

	if out, err := docker.run(ctx, append(env, "restore", "latest", "/work/restore-degraded")...); err != nil {
		t.Fatalf("restore after losing %d node(s) failed: %v\n%s", lostPeers, err, out)
	} else {
		t.Logf("restore after node loss output:\n%s", strings.TrimSpace(out))
	}

	restored := findRestoredFile(t, restoreDir, "degraded-restore.bin")
	actual, err := os.ReadFile(restored)
	if err != nil {
		t.Fatalf("ReadFile restored file returned error: %v", err)
	}
	actualHash := sha256.Sum256(actual)
	if !bytes.Equal(actual, fixtureBytes) {
		t.Fatalf("restored bytes do not match original: got %d bytes sha256=%x, want %d bytes sha256=%x", len(actual), actualHash, len(fixtureBytes), fixtureHash)
	}
	t.Logf("restored file after node loss: %s (%d bytes, sha256=%x)", restored, len(actual), actualHash)
	t.Logf("byte comparison after node loss: restored content matches original fixture")
	t.Logf("summary: backed up %d fixture byte(s), stopped %d/%d storage node(s), restored %d byte(s) without corruption", len(fixtureBytes), lostPeers, len(storagePeers), len(actual))
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
      - type: bind
        source: %s
        target: /work
  node1:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    command: ["--config", "/work/configs/node1.json", "daemon"]
    volumes:
      - type: bind
        source: %s
        target: /work
      - node1-data:/data
  node2:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    command: ["--config", "/work/configs/node2.json", "daemon"]
    volumes:
      - type: bind
        source: %s
        target: /work
      - node2-data:/data
  node3:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    command: ["--config", "/work/configs/node3.json", "daemon"]
    volumes:
      - type: bind
        source: %s
        target: /work
      - node3-data:/data
  client:
    build:
      context: %s
      dockerfile: deploy/docker/Dockerfile
    user: "0:0"
    entrypoint: ["sleep"]
    command: ["3600000"]
    volumes:
      - type: bind
        source: %s
        target: /work

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
			// Docker e2e tests use explicit service-name peers. Keep mDNS disabled
			// here so LAN discovery can be tested separately without container noise.
			"enable_local_discovery":  false,
			"local_discovery_service": "_p2pbk._udp",
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

func setClientBootstrapPeers(t *testing.T, path string, peers []string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile client config returned error: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Unmarshal client config returned error: %v", err)
	}
	network, ok := cfg["network"].(map[string]any)
	if !ok {
		t.Fatal("client config network section is missing or invalid")
	}
	network["bootstrap_peers"] = peers
	network["min_peers"] = len(peers)
	writeConfig(t, path, cfg)
}

func deterministicBytes(size int) []byte {
	out := make([]byte, size)
	for i := range out {
		out[i] = byte((i*31 + i/7 + 17) % 251)
	}
	return out
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
