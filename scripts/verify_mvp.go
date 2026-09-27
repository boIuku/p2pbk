// Command verify_mvp runs independent MVP verification suites and records
// machine-readable evidence. It intentionally does not use a shell pipeline:
// every subprocess exit code is preserved and later suites still run after an
// earlier failure.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type commandSpec struct {
	Name string
	Exe  string
	Args []string
}

type commandEvidence struct {
	Name       string    `json:"name"`
	Command    []string  `json:"command"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	ExitCode   int       `json:"exit_code"`
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	Error      string    `json:"error,omitempty"`
}

type report struct {
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt time.Time         `json:"finished_at"`
	GoVersion  string            `json:"go_version"`
	GOOS       string            `json:"goos"`
	GOARCH     string            `json:"goarch"`
	Evidence   string            `json:"evidence_dir"`
	Commands   []commandEvidence `json:"commands"`
	Contract   []string          `json:"contract_problems,omitempty"`
	Result     string            `json:"result"`
}

var requiredFastContracts = []string{
	"TestDaemonPeerIdentitySurvivesRestart",
	"TestDiscoveryIdentitySurvivesRestart",
	"TestShardAuthorizationAcrossOwners",
	"TestUploadRejectsForgedOrMismatchedReceipt",
	"TestRestoreFromRecoveryBundle",
	"TestMissingManifestCannotInitializeReplacementRepository",
	"TestRepositoryStorageModeIsStable",
	"TestRPCCancelAfterStreamOpened",
	"TestRPCRejectsOversizedMessage",
	"TestRPCServerBoundsUnfinishedRequest",
	"TestLocatorIsolationAcrossRepositories",
	"TestLocatorRejectsWrongKeyAndReplay",
	"TestLocatorReadFailureDoesNotEraseLocations",
	"TestSignedLocatorAcrossRealDHTNodes",
	"TestDurableLocatorSurvivesDHTLoss",
	"TestCommittedBackupSurvivesCrash",
	"TestFailedOverwritePreservesCommittedBlob",
	"TestConcurrentStorageWritersDoNotLoseManifests",
	"TestDoNotRecreateIsAtomicAcrossWriters",
	"TestDefaultPolicyEveryRecoverableShardSubset",
	"TestQuotaSurvivesRestart",
	"TestZeroOfferRejectsRemoteWrites",
	"TestConcurrentShardQuota",
	"TestFailedShardWritePreservesBytesAndQuota",
	"TestRemoteBackupRequiresWritableStorageCapability",
	"TestConnectionsContinuePastStaleAndMalformedPeers",
	"TestRemoteDiscoveryShutdownWithStalledProviderChannel",
	"TestAuditFailuresPersistAndReduceTrust",
}

func main() {
	var evidenceDir string
	var runDocker, runRace, runStress, runFuzz, selfTest bool
	flag.StringVar(&evidenceDir, "evidence", filepath.Join(".test-results", "mvp", time.Now().UTC().Format("20060102T150405Z")), "evidence directory")
	flag.BoolVar(&runDocker, "docker", false, "run Docker contract gates")
	flag.BoolVar(&runRace, "race", false, "run the race suite")
	flag.BoolVar(&runStress, "stress", false, "run shuffled stress tests")
	flag.BoolVar(&runFuzz, "fuzz", false, "run the bounded erasure fuzz gate")
	flag.BoolVar(&selfTest, "self-test", false, "run an intentionally failing evidence fixture")
	flag.Parse()

	if selfTest {
		if err := runFailingFixture(evidenceDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("intentional failing fixture was rejected")
		// The self-test is intentionally a non-zero process so callers can verify
		// that a failing fixture cannot be hidden by a later successful command.
		os.Exit(1)
	}

	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		fatal(err)
	}
	absoluteEvidenceDir, err := filepath.Abs(evidenceDir)
	if err != nil {
		fatal(err)
	}
	evidenceDir = absoluteEvidenceDir
	if err := os.MkdirAll(filepath.Join(evidenceDir, "bin"), 0o700); err != nil {
		fatal(err)
	}
	rep := report{StartedAt: time.Now().UTC(), Evidence: evidenceDir, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}

	commands := []commandSpec{
		{Name: "go-version", Exe: "go", Args: []string{"version"}},
		{Name: "vet", Exe: "go", Args: []string{"vet", "./..."}},
		{Name: "build-p2pbk", Exe: "go", Args: []string{"build", "-buildvcs=false", "-o", filepath.Join(evidenceDir, "bin", exeName("p2pbk")), "./cmd/p2pbk"}},
		{Name: "build-p2pdiscovery", Exe: "go", Args: []string{"build", "-buildvcs=false", "-o", filepath.Join(evidenceDir, "bin", exeName("p2pdiscovery")), "./cmd/p2pdiscovery"}},
		{Name: "smoke-p2pbk-version", Exe: filepath.Join(evidenceDir, "bin", exeName("p2pbk")), Args: []string{"version"}},
		{Name: "smoke-p2pbk-help", Exe: filepath.Join(evidenceDir, "bin", exeName("p2pbk")), Args: []string{"--help"}},
		{Name: "smoke-p2pdiscovery-help", Exe: filepath.Join(evidenceDir, "bin", exeName("p2pdiscovery")), Args: []string{"--help"}},
		{Name: "fast", Exe: "go", Args: []string{"test", "-json", "./...", "-count=1", "-timeout=5m"}},
	}
	if runRace {
		commands = append(commands, commandSpec{Name: "race", Exe: "go", Args: []string{"test", "-race", "-json", "./...", "-count=1", "-timeout=8m"}})
	}
	if runDocker {
		commands = append(commands,
			commandSpec{Name: "docker-info", Exe: "docker", Args: []string{"info"}},
			commandSpec{Name: "docker", Exe: "go", Args: []string{"test", "-tags=docker", "-json", "./test/integration", "-run", "^TestDocker", "-count=1", "-timeout=20m"}},
		)
	}
	if runStress {
		commands = append(commands, commandSpec{Name: "stress", Exe: "go", Args: []string{"test", "-json", "./internal/network/...", "./internal/backend/p2p/...", "-shuffle=on", "-count=20", "-timeout=15m"}})
	}
	if runFuzz {
		commands = append(commands, commandSpec{Name: "fuzz", Exe: "go", Args: []string{"test", "./internal/backend/p2p/erasure", "-run", "^$", "-fuzz", "^FuzzErasureRecovery$", "-fuzztime=60s", "-timeout=2m"}})
	}

	for _, spec := range commands {
		entry := runCommand(evidenceDir, spec)
		rep.Commands = append(rep.Commands, entry)
		if spec.Name == "go-version" && entry.ExitCode == 0 {
			if versionBytes, readErr := os.ReadFile(entry.Stdout); readErr == nil {
				rep.GoVersion = strings.TrimSpace(string(versionBytes))
			}
		}
		if spec.Name == "fast" && entry.ExitCode == 0 {
			problems, err := checkJSON(filepath.Join(evidenceDir, "fast.stdout.jsonl"), requiredFastContracts)
			if err != nil {
				rep.Contract = append(rep.Contract, "fast evidence: "+err.Error())
			} else {
				rep.Contract = append(rep.Contract, problems...)
			}
		}
		if spec.Name == "docker" && entry.ExitCode == 0 {
			problems, err := checkJSON(filepath.Join(evidenceDir, "docker.stdout.jsonl"), []string{
				"TestDockerRemoteDiscoveryBackupRestore",
				"TestDockerRestoreAfterMaxNodeLoss",
				"TestDockerRestoreFailsBeyondParity",
				"TestDockerBackupCreatesSnapshotReceiptsAndRemoteShards",
				"TestDockerRestoreLatest",
			})
			if err != nil {
				rep.Contract = append(rep.Contract, "docker evidence: "+err.Error())
			} else {
				rep.Contract = append(rep.Contract, problems...)
			}
		}
	}

	// Keep the existing Python checker an independent gate when an interpreter
	// exists. Its absence is recorded as a failed prerequisite, never hidden.
	if fast := findCommand(rep.Commands, "fast"); fast != nil {
		checker := findPython()
		if checker == "" {
			rep.Contract = append(rep.Contract, "python checker unavailable: install Python to run scripts/check-test-results.py")
		} else {
			checkerEntry := runCommand(evidenceDir, commandSpec{Name: "python-fast-checker", Exe: checker, Args: []string{"scripts/check-test-results.py", "--suite", "fast", filepath.Join(evidenceDir, "fast.stdout.jsonl")}})
			rep.Commands = append(rep.Commands, checkerEntry)
		}
	}

	rep.FinishedAt = time.Now().UTC()
	failed := false
	for _, command := range rep.Commands {
		if command.ExitCode != 0 {
			failed = true
		}
	}
	if len(rep.Contract) > 0 {
		failed = true
	}
	if failed {
		rep.Result = "failed"
	} else {
		rep.Result = "passed"
	}
	writeReport(evidenceDir, rep)
	if failed {
		os.Exit(1)
	}
}

func runCommand(evidenceDir string, spec commandSpec) commandEvidence {
	started := time.Now().UTC()
	stdoutName := spec.Name + ".stdout"
	if spec.Name == "fast" || spec.Name == "docker" || spec.Name == "race" || spec.Name == "stress" {
		stdoutName += ".jsonl"
	}
	stdoutPath := filepath.Join(evidenceDir, stdoutName)
	stderrPath := filepath.Join(evidenceDir, spec.Name+".stderr.log")
	entry := commandEvidence{Name: spec.Name, Command: append([]string{spec.Exe}, spec.Args...), StartedAt: started, Stdout: stdoutPath, Stderr: stderrPath, ExitCode: -1}
	out, outErr := os.Create(stdoutPath)
	errOut, errErr := os.Create(stderrPath)
	if outErr != nil || errErr != nil {
		entry.Error = fmt.Sprintf("create evidence outputs: %v %v", outErr, errErr)
		entry.FinishedAt = time.Now().UTC()
		return entry
	}
	defer out.Close()
	defer errOut.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Exe, spec.Args...)
	cmd.Stdout = out
	cmd.Stderr = errOut
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(evidenceDir, "go-cache"))
	if err := os.MkdirAll(filepath.Join(evidenceDir, "go-cache"), 0o700); err != nil {
		entry.Error = err.Error()
		entry.FinishedAt = time.Now().UTC()
		return entry
	}
	err := cmd.Run()
	entry.FinishedAt = time.Now().UTC()
	if err == nil {
		entry.ExitCode = 0
		return entry
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		entry.ExitCode = exitErr.ExitCode()
	} else {
		entry.ExitCode = 127
		entry.Error = err.Error()
	}
	return entry
}

func checkJSON(path string, required []string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	passed := map[string]bool{}
	failed := []string{}
	sawEvent := false
	sawPackageStart := false
	sawPackageCompletion := false
	allowedActions := map[string]bool{"start": true, "run": true, "output": true, "pass": true, "fail": true, "skip": true}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		sawEvent = true
		var event struct {
			Action  string `json:"Action"`
			Package string `json:"Package"`
			Test    string `json:"Test"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
		}
		if !allowedActions[event.Action] {
			failed = append(failed, fmt.Sprintf("unknown test action: %s", event.Action))
		}
		if event.Package != "" && event.Action == "start" {
			sawPackageStart = true
		}
		if event.Package != "" && (event.Action == "pass" || event.Action == "fail") && event.Test == "" {
			sawPackageCompletion = true
		}
		if event.Action == "fail" {
			failed = append(failed, fmt.Sprintf("failed: %s/%s", event.Package, event.Test))
		}
		if event.Action == "skip" {
			for _, name := range required {
				if event.Test == name {
					failed = append(failed, "required contract skipped: "+name)
				}
			}
		}
		if event.Action == "pass" && event.Test != "" {
			passed[event.Test] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !sawEvent {
		failed = append(failed, "empty test evidence")
	}
	if !sawPackageStart {
		failed = append(failed, "no package start event was recorded")
	}
	if !sawPackageCompletion {
		failed = append(failed, "no package completion event was recorded; evidence may be truncated")
	}
	for _, name := range required {
		if !passed[name] {
			failed = append(failed, "required contract did not pass: "+name)
		}
	}
	sort.Strings(failed)
	return failed, nil
}

func runFailingFixture(evidenceDir string) error {
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(evidenceDir, "intentional-failure.jsonl")
	data := []byte(`{"Action":"fail","Package":"fixture","Test":"TestIntentionalFailure"}` + "\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	problems, err := checkJSON(path, []string{"TestIntentionalFailure"})
	if err != nil {
		return err
	}
	if len(problems) == 0 {
		return fmt.Errorf("verification accepted an intentionally failing fixture")
	}
	return nil
}

func findPython() string {
	for _, candidate := range []string{"python3", "python"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}

func findCommand(commands []commandEvidence, name string) *commandEvidence {
	for i := range commands {
		if commands[i].Name == name {
			return &commands[i]
		}
	}
	return nil
}

func writeReport(evidenceDir string, rep report) {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidenceDir, "report.json"), append(data, '\n'), 0o600); err != nil {
		fatal(err)
	}
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
