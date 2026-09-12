package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type cancellingOutput struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancellingOutput) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if bytes.Contains(p, []byte("public discovery peer:")) {
		w.cancel()
	}
	return n, err
}

func TestDiscoveryIdentitySurvivesRestart(t *testing.T) {
	opts := options{KeyPath: filepath.Join(t.TempDir(), "network.key"), ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"}}
	var first string
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		out := &cancellingOutput{cancel: cancel}
		err := run(ctx, opts, out)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		line := strings.Split(out.String(), "\n")[0]
		if !strings.Contains(line, "public discovery peer:") {
			t.Fatalf("missing identity: %q", line)
		}
		if i == 0 {
			first = line
		} else if line != first {
			t.Fatalf("restart changed discovery identity: %s -> %s", first, line)
		}
	}
}
