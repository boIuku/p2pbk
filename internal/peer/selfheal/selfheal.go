package selfheal

import (
	"context"
	"errors"
	"fmt"
	"os"

	"p2pbk/internal/peer/blobserver"
)

type ExpectedShard struct {
	Ref  blobserver.ShardRef
	Hash string
}

type Status string

const (
	StatusOK       Status = "ok"
	StatusMissing  Status = "missing"
	StatusCorrupt  Status = "corrupt"
	StatusRepaired Status = "repaired"
	StatusFailed   Status = "failed"
)

type Result struct {
	Shard  ExpectedShard
	Status Status
	Err    error
}

type RepairFunc func(context.Context, ExpectedShard) error

type Scanner struct {
	Root     string
	Expected []ExpectedShard
	Repair   RepairFunc
}

func (s Scanner) Scan(ctx context.Context) []Result {
	results := make([]Result, 0, len(s.Expected))
	for _, expected := range s.Expected {
		results = append(results, s.scanOne(ctx, expected))
	}
	return results
}

func (s Scanner) scanOne(ctx context.Context, expected ExpectedShard) Result {
	if err := expected.Ref.Validate(); err != nil {
		return Result{Shard: expected, Status: StatusFailed, Err: err}
	}
	if expected.Hash == "" {
		return Result{Shard: expected, Status: StatusFailed, Err: errors.New("expected hash is required")}
	}

	path, err := blobserver.ShardPath(s.Root, expected.Ref)
	if err != nil {
		return Result{Shard: expected, Status: StatusFailed, Err: err}
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s.repair(ctx, expected, StatusMissing)
	case err != nil:
		return Result{Shard: expected, Status: StatusFailed, Err: fmt.Errorf("read shard: %w", err)}
	case blobserver.Hash(data) != expected.Hash:
		return s.repair(ctx, expected, StatusCorrupt)
	default:
		return Result{Shard: expected, Status: StatusOK}
	}
}

func (s Scanner) repair(ctx context.Context, expected ExpectedShard, original Status) Result {
	if s.Repair == nil {
		return Result{Shard: expected, Status: original}
	}
	if err := s.Repair(ctx, expected); err != nil {
		return Result{Shard: expected, Status: StatusFailed, Err: err}
	}
	return Result{Shard: expected, Status: StatusRepaired}
}

func Count(results []Result, status Status) int {
	var count int
	for _, result := range results {
		if result.Status == status {
			count++
		}
	}
	return count
}
