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

	"p2pbk/internal/peer/localdb"
)

const (
	dockerE2ELogsEnv = "P2PBK_DOCKER_E2E_LOGS"
	testPassphrase   = "test-repo-pass"
)

var dockerTestImage string

func TestMain(m *testing.M) {
	root, err := repoRootPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[docker-e2e] find repo root: %v\n", err)
		os.Exit(1)
	}
	dockerTestImage = "p2pbk-e2e:" + strings.ToLower(strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	started := time.Now()
	fmt.Fprintf(os.Stderr, "[docker-e2e] building %s\n", dockerTestImage)
	cmd := exec.Command("docker", "build", "-t", dockerTestImage, "-f", "deploy/docker/Dockerfile", ".")
	cmd.Dir = root
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "[docker-e2e] build failed after %s: %v\n", time.Since(started).Round(time.Millisecond), err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[docker-e2e] build finished in %s\n", time.Since(started).Round(time.Millisecond))
	os.Exit(m.Run())
}

type dockerClusterSpec struct {
	NodeCount    int
	DataShards   int
	ParityShards int
}

type dockerCluster struct {
	t           *testing.T
	work        string
	configDir   string
	fixtureDir  string
	restoreDir  string
	clientDir   string
	composePath string
	docker      dockerCompose

	dataShards   int
	parityShards int
	storagePeers []string
}

type dockerCompose struct {
	t           *testing.T
	project     string
	composePath string
}

type nodeConfigOptions struct {
	BootstrapPeers       []string
	PublicDiscoveryPeers []string
	Discovery            string
	OfferBytes           int64
	MinPeers             int
	DataShards           int
	ParityShards         int
}

func newDockerCluster(t *testing.T, ctx context.Context, spec dockerClusterSpec) *dockerCluster {
	t.Helper()
	if spec.NodeCount == 0 {
		spec.NodeCount = 3
	}
	if spec.DataShards == 0 {
		spec.DataShards = 2
	}
	if spec.ParityShards == 0 {
		spec.ParityShards = 1
	}

	work := t.TempDir()
	cluster := &dockerCluster{
		t:            t,
		work:         work,
		configDir:    filepath.Join(work, "configs"),
		fixtureDir:   filepath.Join(work, "fixtures"),
		restoreDir:   filepath.Join(work, "restore"),
		clientDir:    filepath.Join(work, "client"),
		composePath:  filepath.Join(work, "compose.yaml"),
		dataShards:   spec.DataShards,
		parityShards: spec.ParityShards,
	}
	for _, dir := range []string{
		cluster.configDir,
		cluster.fixtureDir,
		cluster.restoreDir,
		cluster.clientDir,
		filepath.Join(work, "nodes", "bootstrap", "data"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) returned error: %v", dir, err)
		}
	}
	for i := 1; i <= spec.NodeCount; i++ {
		if err := os.MkdirAll(cluster.nodeDataDir(fmt.Sprintf("node%d", i)), 0o755); err != nil {
			t.Fatalf("MkdirAll node data returned error: %v", err)
		}
	}

	writeDockerCompose(t, cluster.composePath, work, spec.NodeCount)
	cluster.writeNodeConfig("bootstrap", nodeConfigOptions{
		Discovery:    "disabled",
		OfferBytes:   0,
		DataShards:   spec.DataShards,
		ParityShards: spec.ParityShards,
	})
	cluster.docker = dockerCompose{t: t, project: dockerProjectName(), composePath: cluster.composePath}
	if out, err := cluster.docker.run(ctx, "version"); err != nil {
		t.Fatalf("docker compose version failed: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if os.Getenv(dockerE2ELogsEnv) == "1" {
			cluster.docker.dumpLogs(cleanupCtx)
		}
		if out, err := cluster.docker.run(cleanupCtx, "down", "-v", "--remove-orphans"); err != nil {
			t.Logf("docker compose down failed: %v\n%s", err, out)
		}
	})
	return cluster
}

func (c *dockerCluster) startExplicitStorageCluster(ctx context.Context) {
	c.t.Helper()
	if out, err := c.docker.run(ctx, "up", "-d", "bootstrap"); err != nil {
		c.t.Fatalf("docker compose up bootstrap failed: %v\n%s", err, out)
	}
	bootstrapID := c.docker.waitForLogMatch(ctx, "bootstrap", regexp.MustCompile(`p2pbk bootstrap peer:\s+(\S+)`))
	bootstrapAddr := fmt.Sprintf("/dns4/bootstrap/tcp/4001/p2p/%s", bootstrapID)
	c.t.Logf("bootstrap peer: %s", bootstrapAddr)

	services := []string{"client"}
	for i := 1; i <= c.nodeCount(); i++ {
		name := fmt.Sprintf("node%d", i)
		c.writeNodeConfig(name, nodeConfigOptions{
			BootstrapPeers: []string{bootstrapAddr},
			Discovery:      "disabled",
			OfferBytes:     10_000_000_000,
			DataShards:     c.dataShards,
			ParityShards:   c.parityShards,
		})
		services = append(services, name)
	}
	args := append([]string{"up", "-d"}, services...)
	if out, err := c.docker.run(ctx, args...); err != nil {
		c.t.Fatalf("docker compose up nodes failed: %v\n%s", err, out)
	}

	c.storagePeers = nil
	for i := 1; i <= c.nodeCount(); i++ {
		service := fmt.Sprintf("node%d", i)
		peerID := c.waitForDaemonPeerID(ctx, service)
		peerAddr := fmt.Sprintf("/dns4/%s/tcp/4001/p2p/%s", service, peerID)
		c.storagePeers = append(c.storagePeers, peerAddr)
		c.t.Logf("storage peer %s: %s", service, peerAddr)
	}
}

func (c *dockerCluster) startRemoteDiscoveryNodes(ctx context.Context) {
	c.t.Helper()
	if out, err := c.docker.run(ctx, "up", "-d", "bootstrap"); err != nil {
		c.t.Fatalf("docker compose up bootstrap failed: %v\n%s", err, out)
	}
	bootstrapID := c.docker.waitForLogMatch(ctx, "bootstrap", regexp.MustCompile(`p2pbk bootstrap peer:\s+(\S+)`))
	publicDiscoveryPeer := fmt.Sprintf("/dns4/bootstrap/tcp/4001/p2p/%s", bootstrapID)

	services := []string{"client"}
	for i := 1; i <= c.nodeCount(); i++ {
		name := fmt.Sprintf("node%d", i)
		c.writeNodeConfig(name, nodeConfigOptions{
			PublicDiscoveryPeers: []string{publicDiscoveryPeer},
			Discovery:            "remote",
			OfferBytes:           10_000_000_000,
			DataShards:           c.dataShards,
			ParityShards:         c.parityShards,
		})
		services = append(services, name)
	}
	args := append([]string{"up", "-d"}, services...)
	if out, err := c.docker.run(ctx, args...); err != nil {
		c.t.Fatalf("docker compose up remote discovery nodes failed: %v\n%s", err, out)
	}
	for i := 1; i <= c.nodeCount(); i++ {
		service := fmt.Sprintf("node%d", i)
		c.waitForDaemonPeerID(ctx, service)
		c.docker.waitForLogMatch(ctx, service, regexp.MustCompile(`"event":"remote_discovery_started"`))
	}
}

func (c *dockerCluster) initClient(ctx context.Context, peers []string) {
	c.t.Helper()
	args := []string{
		"exec", "-T", "client", "p2pbk",
		"--config", "/work/client/config.json",
		"init",
		"--data-dir", "/work/client/data",
		"--dir", "/work/fixtures",
		"--max-space", "10GB",
		"--offer-space", "0GB",
		"--schedule", "daily",
		"--passphrase", testPassphrase,
		"--start=false",
	}
	for _, peer := range peers {
		args = append(args, "--bootstrap-peer", peer)
	}
	if out, err := c.docker.run(ctx, args...); err != nil {
		c.t.Fatalf("client init failed: %v\n%s", err, out)
	} else {
		c.t.Logf("client init output:\n%s", strings.TrimSpace(out))
	}
}

func (c *dockerCluster) repoInit(ctx context.Context) {
	c.t.Helper()
	if out, err := c.runClient(ctx, "repo", "init"); err != nil {
		c.t.Fatalf("repo init failed: %v\n%s", err, out)
	} else {
		c.t.Logf("repo init output:\n%s", strings.TrimSpace(out))
	}
}

func (c *dockerCluster) backupNow(ctx context.Context) {
	c.t.Helper()
	if out, err := c.runClient(ctx, "backup", "now"); err != nil {
		c.t.Fatalf("backup now failed: %v\n%s", err, out)
	} else {
		c.t.Logf("backup output:\n%s", strings.TrimSpace(out))
	}
}

func (c *dockerCluster) restoreLatest(ctx context.Context, target string) {
	c.t.Helper()
	if out, err := c.runClient(ctx, "restore", "latest", target); err != nil {
		c.t.Fatalf("restore latest failed: %v\n%s", err, out)
	} else {
		c.t.Logf("restore output:\n%s", strings.TrimSpace(out))
	}
}

func (c *dockerCluster) runClient(ctx context.Context, args ...string) (string, error) {
	c.t.Helper()
	base := []string{
		"exec", "-T",
		"-e", "P2PBK_REPOSITORY_PASSWORD=" + testPassphrase,
		"client", "p2pbk", "--config", "/work/client/config.json",
	}
	return c.docker.run(ctx, append(base, args...)...)
}

func (c *dockerCluster) waitForDaemonPeerID(ctx context.Context, service string) string {
	c.t.Helper()
	return c.docker.waitForLogMatch(ctx, service, regexp.MustCompile(`"event":"daemon_started".*"peer_id":"([^"]+)"`))
}

func (c *dockerCluster) writeTextFixture(rel, text string) {
	c.t.Helper()
	path := filepath.Join(c.fixtureDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		c.t.Fatalf("MkdirAll fixture returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		c.t.Fatalf("WriteFile fixture returned error: %v", err)
	}
}

func (c *dockerCluster) writeBinaryFixture(rel string, data []byte) {
	c.t.Helper()
	path := filepath.Join(c.fixtureDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		c.t.Fatalf("MkdirAll fixture returned error: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		c.t.Fatalf("WriteFile fixture returned error: %v", err)
	}
}

func (c *dockerCluster) assertRestoredBytes(name string, want []byte) {
	c.t.Helper()
	restored := findRestoredFile(c.t, c.restoreDir, name)
	actual, err := os.ReadFile(restored)
	if err != nil {
		c.t.Fatalf("ReadFile restored file returned error: %v", err)
	}
	if !bytes.Equal(actual, want) {
		c.t.Fatalf("restored bytes do not match original: got %d bytes sha256=%x, want %d bytes sha256=%x",
			len(actual), sha256.Sum256(actual), len(want), sha256.Sum256(want))
	}
}

func (c *dockerCluster) assertClientReceiptsAtLeast(want int) {
	c.t.Helper()
	db, err := localdb.Open(filepath.Join(c.clientDir, "data", "local.db"))
	if err != nil {
		c.t.Fatalf("open client localdb: %v", err)
	}
	defer db.Close()
	receipts, err := db.ListShardReceipts()
	if err != nil {
		c.t.Fatalf("ListShardReceipts returned error: %v", err)
	}
	if len(receipts) < want {
		c.t.Fatalf("receipt count = %d, want at least %d", len(receipts), want)
	}
}

func (c *dockerCluster) assertKnownPeersAtLeast(want int) {
	c.t.Helper()
	db, err := localdb.Open(filepath.Join(c.clientDir, "data", "local.db"))
	if err != nil {
		c.t.Fatalf("open client localdb: %v", err)
	}
	defer db.Close()
	peers, err := db.ListPeers()
	if err != nil {
		c.t.Fatalf("ListPeers returned error: %v", err)
	}
	if len(peers) < want {
		c.t.Fatalf("known peer count = %d, want at least %d", len(peers), want)
	}
}

func (c *dockerCluster) assertStoredShardFilesAtLeast(want int) {
	c.t.Helper()
	var count int
	root := filepath.Join(c.work, "nodes")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(d.Name(), ".shard") {
			count++
		}
		return nil
	})
	if err != nil {
		c.t.Fatalf("WalkDir shard stores returned error: %v", err)
	}
	if count < want {
		c.t.Fatalf("stored shard file count = %d, want at least %d", count, want)
	}
}

func (c *dockerCluster) setClientBootstrapPeers(peers []string) {
	c.t.Helper()
	updateConfig(c.t, filepath.Join(c.clientDir, "config.json"), func(cfg map[string]any) {
		network := cfg["network"].(map[string]any)
		network["bootstrap_peers"] = peers
		network["min_peers"] = len(peers)
	})
}

func (c *dockerCluster) writeNodeConfig(name string, opts nodeConfigOptions) {
	c.t.Helper()
	if opts.Discovery == "" {
		opts.Discovery = "disabled"
	}
	if opts.DataShards == 0 {
		opts.DataShards = c.dataShards
	}
	if opts.ParityShards == 0 {
		opts.ParityShards = c.parityShards
	}
	writeConfig(c.t, filepath.Join(c.configDir, name+".json"), dockerNodeConfig("/work/nodes/"+name+"/data", opts))
}

func (c *dockerCluster) nodeDataDir(name string) string {
	return filepath.Join(c.work, "nodes", name, "data")
}

func (c *dockerCluster) nodeCount() int {
	count := 0
	for {
		count++
		if _, err := os.Stat(filepath.Join(c.work, "nodes", fmt.Sprintf("node%d", count), "data")); err != nil {
			return count - 1
		}
	}
}

func (d dockerCompose) run(ctx context.Context, args ...string) (string, error) {
	d.t.Helper()
	fullArgs := append([]string{"compose", "-p", d.project, "-f", d.composePath}, args...)
	started := time.Now()
	fmt.Fprintf(os.Stderr, "[docker-e2e:%s] docker %s\n", d.project, strings.Join(fullArgs, " "))
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	status := "ok"
	if err != nil {
		status = "failed"
	}
	fmt.Fprintf(os.Stderr, "[docker-e2e:%s] %s in %s: docker %s\n", d.project, status, time.Since(started).Round(time.Millisecond), strings.Join(fullArgs, " "))
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

func dockerNodeConfig(dataDir string, opts nodeConfigOptions) map[string]any {
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
			"offer_bytes":      opts.OfferBytes,
		},
		"network": map[string]any{
			"listen_addrs": []string{
				"/ip4/0.0.0.0/tcp/4001",
			},
			"bootstrap_peers":                      nonNilStrings(opts.BootstrapPeers),
			"public_discovery_peers":               nonNilStrings(opts.PublicDiscoveryPeers),
			"discovery":                            opts.Discovery,
			"protocol_id":                          "/p2pbk/1.0.0",
			"min_peers":                            opts.MinPeers,
			"local_discovery_service":              "_p2pbk._udp",
			"remote_discovery_namespace":           "p2pbk/storage/v1",
			"remote_discovery_interval":            "1s",
			"force_private_reachability_for_relay": false,
		},
		"backup": map[string]any{
			"data_shards":   opts.DataShards,
			"parity_shards": opts.ParityShards,
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

func writeDockerCompose(t *testing.T, path, work string, nodeCount int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "services:\n")
	writeService(&b, "bootstrap", work, []string{"--config", "/work/configs/bootstrap.json", "bootstrap"}, false)
	for i := 1; i <= nodeCount; i++ {
		name := fmt.Sprintf("node%d", i)
		writeService(&b, name, work, []string{"--config", "/work/configs/" + name + ".json", "daemon"}, false)
	}
	writeService(&b, "client", work, []string{"3600000"}, true)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("WriteFile compose returned error: %v", err)
	}
}

func writeService(b *strings.Builder, name, work string, command []string, sleepEntrypoint bool) {
	fmt.Fprintf(b, "  %s:\n", name)
	fmt.Fprintf(b, "    image: %s\n", yamlQuote(dockerTestImage))
	fmt.Fprintf(b, "    user: \"0:0\"\n")
	if sleepEntrypoint {
		fmt.Fprintf(b, "    entrypoint: [\"sleep\"]\n")
	}
	fmt.Fprintf(b, "    command: %s\n", jsonArray(command))
	fmt.Fprintf(b, "    volumes:\n")
	fmt.Fprintf(b, "      - type: bind\n")
	fmt.Fprintf(b, "        source: %s\n", yamlQuote(filepath.ToSlash(work)))
	fmt.Fprintf(b, "        target: /work\n")
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

func updateConfig(t *testing.T, path string, update func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile config returned error: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Unmarshal config returned error: %v", err)
	}
	update(cfg)
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
	root, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func repoRootPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("Getwd returned error: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return "", fmt.Errorf("could not find repo root")
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

func jsonArray(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
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
