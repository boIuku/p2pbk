package app

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"time"

	"p2pbk/internal/audit"
	"p2pbk/internal/config"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/localdb"
)

type AuditRequest struct {
	Config  config.Config
	Limit   int
	Timeout time.Duration
}

type AuditResult struct {
	Checked int
	OK      int
	Failed  int
	Output  string
}

func RunAudit(ctx context.Context, req AuditRequest) (AuditResult, error) {
	paths, err := req.Config.Paths()
	if err != nil {
		return AuditResult{}, err
	}
	db, err := localdb.Open(paths.LocalDB)
	if err != nil {
		return AuditResult{}, err
	}
	defer db.Close()

	host, err := p2phost.New(ctx, p2phost.Options{KeyPath: filepath.Join(paths.DataDir, "network.key")})
	if err != nil {
		return AuditResult{}, err
	}
	defer host.Close()

	var out bytes.Buffer
	summary, err := audit.RunOnce(ctx, audit.Options{
		Config:         req.Config,
		Host:           host,
		DB:             db,
		Output:         &out,
		Limit:          req.Limit,
		ConnectTimeout: req.Timeout,
		ShardTimeout:   req.Timeout,
	})
	if err != nil {
		return AuditResult{}, err
	}
	fmt.Fprintf(&out, "audit summary: checked=%d ok=%d failed=%d\n", summary.Checked, summary.Succeeded, summary.Failed)
	return AuditResult{
		Checked: summary.Checked,
		OK:      summary.Succeeded,
		Failed:  summary.Failed,
		Output:  out.String(),
	}, nil
}
