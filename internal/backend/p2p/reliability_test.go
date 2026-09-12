package p2p

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kopia/kopia/repo/blob"
	"p2pbk/internal/durable"
	"p2pbk/internal/peer/blobserver"
)

type failingBackend struct {
	ShardBackend
	puts   int
	failAt int
}

func (b *failingBackend) PutShard(ctx context.Context, req blobserver.PutShardRequest) (blobserver.PutShardResponse, error) {
	b.puts++
	if b.puts == b.failAt {
		return blobserver.PutShardResponse{}, errors.New("injected storage failure")
	}
	return b.ShardBackend.PutShard(ctx, req)
}

// P0-07: a failed overwrite must leave the last acknowledged value available,
// including after reopen. Returning an error is not sufficient if old bytes died.
func TestFailedOverwritePreservesCommittedBlob(t *testing.T) {
	for _, phase := range []string{"shard_1", "shard_2", "shard_3", "manifest"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			server, err := blobserver.New(filepath.Join(root, "store"))
			if err != nil {
				t.Fatal(err)
			}
			backend := &failingBackend{ShardBackend: server}
			options := Options{Owner: "owner", DataShards: 2, ParityShards: 1, ManifestPath: filepath.Join(root, "manifests.json")}
			s, err := NewStorage(backend, options)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.PutBlob(ctx, "blob", testBytes("acknowledged original"), blob.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			if phase == "manifest" {
				s.writeFile = func(path string, data []byte, mode os.FileMode) error {
					if path == options.ManifestPath {
						return errors.New("injected disk full")
					}
					return durable.WriteFile(path, data, mode)
				}
			} else {
				var n int
				fmt.Sscanf(phase, "shard_%d", &n)
				backend.failAt = backend.puts + n
			}
			if err = s.PutBlob(ctx, "blob", testBytes("a different replacement"), blob.PutOptions{}); err == nil {
				t.Fatal("fault injection did not fail overwrite")
			}
			s.Close(ctx)
			backend.failAt = 0
			s, err = NewStorage(backend, options)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(ctx)
			var out testOutput
			if err = s.GetBlob(ctx, "blob", 0, -1, &out); err != nil || string(out.bytes) != "acknowledged original" {
				t.Fatalf("failed overwrite destroyed committed data: %q %v", out.bytes, err)
			}
			if err = s.PutBlob(ctx, "blob", testBytes("a different replacement"), blob.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			if err = s.GetBlob(ctx, "blob", 0, -1, &out); err != nil || string(out.bytes) != "a different replacement" {
				t.Fatalf("retry failed: %q %v", out.bytes, err)
			}
		})
	}
}

type crashingBackend struct {
	ShardBackend
	phase string
}

func (b crashingBackend) PutShard(ctx context.Context, req blobserver.PutShardRequest) (blobserver.PutShardResponse, error) {
	r, err := b.ShardBackend.PutShard(ctx, req)
	if err == nil && b.phase == "shard" {
		os.Exit(86)
	}
	return r, err
}

// This subprocess exits without defers at deterministic durability boundaries.
// It never uses user paths; the parent supplies a generated test directory.
func TestStorageCrashProcess(t *testing.T) {
	root := os.Getenv("P2PBK_TEST_CRASH_ROOT")
	if root == "" {
		return
	}
	phase := os.Getenv("P2PBK_TEST_CRASH_PHASE")
	server, err := blobserver.New(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "manifests.json")
	s, err := NewStorage(crashingBackend{server, phase}, Options{Owner: "owner", DataShards: 2, ParityShards: 1, ManifestPath: path})
	if err != nil {
		t.Fatal(err)
	}
	s.writeFile = func(name string, data []byte, mode os.FileMode) error {
		if phase == "before_manifest" && name == path {
			os.Exit(86)
		}
		err := durable.WriteFile(name, data, mode)
		if err == nil && ((phase == "committed_manifest" && name == path) || (phase == "journal" && strings.HasSuffix(name, ".pending"))) {
			os.Exit(86)
		}
		return err
	}
	if err = s.PutBlob(context.Background(), "blob", testBytes("replacement"), blob.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash point not reached")
}

func TestCommittedBackupSurvivesCrash(t *testing.T) {
	for _, phase := range []string{"journal", "shard", "before_manifest", "committed_manifest"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			server, err := blobserver.New(filepath.Join(root, "store"))
			if err != nil {
				t.Fatal(err)
			}
			options := Options{Owner: "owner", DataShards: 2, ParityShards: 1, ManifestPath: filepath.Join(root, "manifests.json")}
			s, err := NewStorage(server, options)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err = s.PutBlob(ctx, "blob", testBytes("original"), blob.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			s.Close(ctx)
			processCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(processCtx, os.Args[0], "-test.run=^TestStorageCrashProcess$")
			cmd.Env = append(os.Environ(), "P2PBK_TEST_CRASH_ROOT="+root, "P2PBK_TEST_CRASH_PHASE="+phase)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 86 {
				t.Fatalf("expected injected crash 86, got %v: %s", err, output)
			}
			s, err = NewStorage(server, options)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(ctx)
			want := "original"
			if phase == "committed_manifest" {
				want = "replacement"
			}
			var out testOutput
			if err = s.GetBlob(ctx, "blob", 0, -1, &out); err != nil || string(out.bytes) != want {
				t.Fatalf("crash recovery got %q %v, want %q", out.bytes, err, want)
			}
			count := 0
			err = filepath.WalkDir(filepath.Join(root, "store"), func(_ string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if strings.HasSuffix(d.Name(), ".shard") {
					count++
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if count != 3 {
				t.Fatalf("crash recovery left %d shard files, want one 3-shard generation", count)
			}
		})
	}
}

// Two independent Storage instances represent separate CLI writers. They must
// merge committed state, not each replace the index from a stale cached map.
func TestConcurrentStorageWritersDoNotLoseManifests(t *testing.T) {
	root := t.TempDir()
	server, err := blobserver.New(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Owner: "owner", DataShards: 2, ParityShards: 1, ManifestPath: filepath.Join(root, "manifests.json")}
	a, err := NewStorage(server, options)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewStorage(server, options)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, s := range []*Storage{a, b} {
		wg.Add(1)
		go func(i int, s *Storage) {
			defer wg.Done()
			<-start
			for n := 0; n < 5; n++ {
				id := blob.ID(fmt.Sprintf("%d-%d", i, n))
				if err := s.PutBlob(context.Background(), id, testBytes(id), blob.PutOptions{DoNotRecreate: true}); err != nil {
					t.Error(err)
				}
			}
		}(i, s)
	}
	close(start)
	wg.Wait()
	var ids []blob.ID
	if err = a.ListBlobs(context.Background(), "", func(m blob.Metadata) error { ids = append(ids, m.BlobID); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 10 {
		t.Fatalf("lost committed writes: %v", ids)
	}
}

func TestDoNotRecreateIsAtomicAcrossWriters(t *testing.T) {
	root := t.TempDir()
	server, _ := blobserver.New(filepath.Join(root, "store"))
	opt := Options{Owner: "owner", DataShards: 2, ParityShards: 1, ManifestPath: filepath.Join(root, "manifests.json")}
	a, err := NewStorage(server, opt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewStorage(server, opt)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, s := range []*Storage{a, b} {
		go func(s *Storage) {
			<-start
			results <- s.PutBlob(context.Background(), "one", testBytes("value"), blob.PutOptions{DoNotRecreate: true})
		}(s)
	}
	close(start)
	success, exists := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, blob.ErrBlobAlreadyExists) {
			exists++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || exists != 1 {
		t.Fatalf("success=%d already_exists=%d", success, exists)
	}
}
