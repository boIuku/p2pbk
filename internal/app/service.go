package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"p2pbk/internal/durable"
	"p2pbk/internal/secrets"
)

const serviceName = "p2pbk"

type ServiceOptions struct {
	ConfigPath     string
	CredentialFile string
	Executable     string
}

// ServiceAction manages the native unattended service. Installation only
// writes service-manager metadata and credential references; it never writes a
// repository password to config, logs, or command output.
func ServiceAction(ctx context.Context, action string, opts ServiceOptions) (string, error) {
	if opts.ConfigPath == "" {
		return "", errors.New("config path is required")
	}
	if opts.CredentialFile == "" && action == "install" {
		return "", errors.New("credential file is required; use an OS-protected service credential")
	}
	if opts.Executable == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve executable: %w", err)
		}
		opts.Executable = executable
	}
	switch runtime.GOOS {
	case "windows":
		return windowsServiceAction(ctx, action, opts)
	case "linux":
		return systemdServiceAction(ctx, action, opts)
	default:
		return "", fmt.Errorf("native service lifecycle is unsupported on %s", runtime.GOOS)
	}
}

func WriteServiceCredential(path, password string) error {
	if path == "" {
		return errors.New("credential file path is required")
	}
	return secrets.WriteCredential(filepath.Clean(path), password)
}

func systemdUnit(opts ServiceOptions) string {
	executable := quoteSystemd(opts.Executable)
	config := quoteSystemd(opts.ConfigPath)
	credential := quoteSystemd(opts.CredentialFile)
	return strings.Join([]string{
		"[Unit]",
		"Description=p2pbk backup service",
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=" + executable + " --config " + config + " daemon --credential-file=%d/repository-password",
		"LoadCredential=repository-password:" + credential,
		"Restart=on-failure",
		"RestartSec=5s",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
}

func systemdServiceAction(ctx context.Context, action string, opts ServiceOptions) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	unitPath := filepath.Join(home, ".config", "systemd", "user", serviceName+".service")
	if action == "install" {
		if err := durable.WriteFile(unitPath, []byte(systemdUnit(opts)), 0o600); err != nil {
			return "", fmt.Errorf("write systemd unit: %w", err)
		}
		if output, err := runServiceCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return output, err
		}
		return runServiceCommand(ctx, "systemctl", "--user", "enable", serviceName+".service")
	}
	if action == "uninstall" {
		output, stopErr := runServiceCommand(ctx, "systemctl", "--user", "disable", "--now", serviceName+".service")
		if stopErr != nil && !strings.Contains(strings.ToLower(output), "not found") {
			return output, stopErr
		}
		if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
			return output, err
		}
		_, reloadErr := runServiceCommand(ctx, "systemctl", "--user", "daemon-reload")
		return output, reloadErr
	}
	args := []string{"--user", action, serviceName + ".service"}
	return runServiceCommand(ctx, "systemctl", args...)
}

func quoteSystemd(value string) string {
	return strings.ReplaceAll(value, " ", "\\ ")
}

func windowsServiceAction(ctx context.Context, action string, opts ServiceOptions) (string, error) {
	serviceBinary := fmt.Sprintf(`"%s" --config "%s" daemon --credential-file "%s"`, opts.Executable, opts.ConfigPath, opts.CredentialFile)
	switch action {
	case "install":
		if _, err := runServiceCommand(ctx, "sc.exe", "create", serviceName, "binPath=", serviceBinary, "start=", "auto", "obj=", "LocalSystem"); err != nil {
			return "", err
		}
		return runServiceCommand(ctx, "sc.exe", "failure", serviceName, "reset=", "86400", "actions=", "restart/60000/restart/60000/\"\"")
	case "uninstall":
		output, stopErr := runServiceCommand(ctx, "sc.exe", "stop", serviceName)
		if stopErr != nil && !strings.Contains(strings.ToLower(output), "not started") {
			return output, stopErr
		}
		return runServiceCommand(ctx, "sc.exe", "delete", serviceName)
	default:
		return runServiceCommand(ctx, "sc.exe", action, serviceName)
	}
}

func runServiceCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	data, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(data))
	if err != nil {
		return output, fmt.Errorf("%s: %w: %s", name, err, output)
	}
	return output, nil
}
