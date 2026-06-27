package audit

import (
	"context"
	"fmt"
	"io"
	"time"

	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"p2pbk/internal/backend/p2p/dispatcher"
	"p2pbk/internal/config"
	"p2pbk/internal/logging"
	p2pdiscovery "p2pbk/internal/network/discovery"
	p2phost "p2pbk/internal/network/host"
	"p2pbk/internal/peer/blobserver"
	"p2pbk/internal/peer/localdb"
	"p2pbk/internal/trust/receipts"
)

type Result struct {
	PeerID   string
	Ref      blobserver.ShardRef
	Success  bool
	Err      error
	Duration time.Duration
}

type Summary struct {
	Checked   int
	Succeeded int
	Failed    int
}

type Options struct {
	Config         config.Config
	Host           corehost.Host
	DB             *localdb.DB
	Output         io.Writer
	Now            func() time.Time
	Limit          int
	ConnectTimeout time.Duration
	ShardTimeout   time.Duration
	Logger         *logging.Logger
}

func RunOnce(ctx context.Context, opts Options) (Summary, error) {
	if opts.Host == nil {
		return Summary{}, fmt.Errorf("host is required")
	}
	if opts.DB == nil {
		return Summary{}, fmt.Errorf("localdb is required")
	}
	connectTimeout := opts.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 2 * time.Second
	}
	connectCtx, cancelConnect := context.WithTimeout(ctx, connectTimeout)
	publicDiscoveryInfos, err := p2phost.ParseAddrInfos(opts.Config.Network.PublicDiscoveryPeers)
	if err != nil {
		cancelConnect()
		return Summary{}, fmt.Errorf("parse public discovery peer: %w", err)
	}
	infrastructureFilter := p2pdiscovery.NewPeerFilter(publicDiscoveryInfos)
	_, err = p2pdiscovery.ConnectConfiguredAndSaved(connectCtx, opts.Host, opts.Config.Network.PublicDiscoveryPeers, nil)
	if err == nil {
		_, err = p2pdiscovery.ConnectConfiguredAndSaved(connectCtx, opts.Host, opts.Config.Network.BootstrapPeers, opts.DB, p2pdiscovery.WithIgnoredPeers(infrastructureFilter))
	}
	cancelConnect()
	if err != nil {
		return Summary{}, err
	}

	receipts, err := opts.DB.ListShardReceipts()
	if err != nil {
		return Summary{}, err
	}
	if opts.Limit > 0 && len(receipts) > opts.Limit {
		receipts = receipts[:opts.Limit]
	}

	dispatch := dispatcher.New(opts.Host)
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	var summary Summary
	for _, receipt := range receipts {
		summary.Checked++
		started := time.Now()
		err := auditReceipt(ctx, dispatch, receipt, opts.ShardTimeout)
		duration := time.Since(started)
		record := localdb.AuditRecord{
			PeerID:    receipt.PeerID,
			Ref:       blobRef(receipt.Receipt.Ref),
			Success:   err == nil,
			CheckedAt: now().UTC(),
		}
		if err != nil {
			record.Error = err.Error()
			summary.Failed++
		} else {
			summary.Succeeded++
		}
		if writeErr := opts.DB.RecordAudit(record); writeErr != nil && err == nil {
			return summary, writeErr
		}
		result := Result{PeerID: receipt.PeerID, Ref: blobRef(receipt.Receipt.Ref), Success: err == nil, Err: err, Duration: duration}
		if opts.Logger != nil {
			writeStructuredResult(opts.Logger, result)
		} else if opts.Output != nil {
			writeResult(opts.Output, result)
		}
	}
	return summary, nil
}

func Start(ctx context.Context, opts Options, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				summary, err := RunOnce(ctx, opts)
				if opts.Logger != nil {
					if err != nil {
						opts.Logger.Error("audit_run_failed", "audit failed", "audit_failed", err, nil)
					} else if summary.Checked > 0 {
						opts.Logger.Info("audit_run_completed", "audit summary", logging.Field{"checked": summary.Checked, "ok": summary.Succeeded, "failed": summary.Failed})
					}
				} else if opts.Output != nil {
					if err != nil {
						fmt.Fprintf(opts.Output, "audit failed: %v\n", err)
					} else if summary.Checked > 0 {
						fmt.Fprintf(opts.Output, "audit summary: checked=%d ok=%d failed=%d\n", summary.Checked, summary.Succeeded, summary.Failed)
					}
				}
			}
		}
	}()
}

func auditReceipt(ctx context.Context, dispatch *dispatcher.Dispatcher, receipt localdb.ShardReceiptRecord, timeout time.Duration) error {
	target, err := peer.Decode(receipt.PeerID)
	if err != nil {
		return fmt.Errorf("decode peer id: %w", err)
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	shardCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	got, err := dispatch.GetShard(shardCtx, target, blobserver.GetShardRequest{Ref: blobRef(receipt.Receipt.Ref)})
	if err != nil {
		return err
	}
	if got.Hash != receipt.Receipt.Hash {
		return fmt.Errorf("shard hash mismatch: got %s want %s", got.Hash, receipt.Receipt.Hash)
	}
	return nil
}

func blobRef(ref receipts.ShardRef) blobserver.ShardRef {
	return blobserver.ShardRef{Owner: ref.Owner, BlobID: ref.BlobID, Index: ref.Index}
}

func writeResult(out io.Writer, result Result) {
	status := "ok"
	if !result.Success {
		status = "failed"
	}
	fmt.Fprintf(out, "audit %s peer=%s blob=%s shard=%d", status, result.PeerID, result.Ref.BlobID, result.Ref.Index)
	if result.Err != nil {
		fmt.Fprintf(out, " error=%q", result.Err.Error())
	}
	fmt.Fprintln(out)
}

func writeStructuredResult(logger *logging.Logger, result Result) {
	level := "info"
	event := "audit_shard_ok"
	errorCode := ""
	message := "audit shard ok"
	errText := ""
	if !result.Success {
		level = "warn"
		event = "audit_shard_failed"
		errorCode = "shard_audit_failed"
		message = "audit shard failed"
		if result.Err != nil {
			errText = result.Err.Error()
		}
	}
	logger.Write(logging.Event{
		Level:      level,
		Event:      event,
		Message:    message,
		PeerID:     result.PeerID,
		ShardRef:   &logging.ShardRef{Owner: result.Ref.Owner, BlobID: result.Ref.BlobID, Index: result.Ref.Index},
		DurationMS: result.Duration.Milliseconds(),
		ErrorCode:  errorCode,
		Error:      errText,
	})
}
