package main

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
)

func TestOptionsFromEnvDefaults(t *testing.T) {
	t.Setenv(envListenAddrs, "")
	t.Setenv(envAnnounceAddrs, "")
	t.Setenv(envNATPortMap, "")
	t.Setenv(envRelayService, "")

	opts := optionsFromEnv()
	if !reflect.DeepEqual(opts.ListenAddrs, []string{defaultListenAddr}) {
		t.Fatalf("ListenAddrs = %#v, want default", opts.ListenAddrs)
	}
	if len(opts.AnnounceAddrs) != 0 {
		t.Fatalf("AnnounceAddrs = %#v, want empty", opts.AnnounceAddrs)
	}
	if !opts.EnableNATPortMap {
		t.Fatal("EnableNATPortMap = false, want true")
	}
	if !opts.EnableRelayService {
		t.Fatal("EnableRelayService = false, want true")
	}
}

func TestOptionsFromEnvOverrides(t *testing.T) {
	t.Setenv(envListenAddrs, "/ip4/0.0.0.0/tcp/4001, /ip6/::/tcp/4001")
	t.Setenv(envAnnounceAddrs, "/dns4/discovery.example.com/tcp/4001")
	t.Setenv(envNATPortMap, "false")
	t.Setenv(envRelayService, "false")

	opts := optionsFromEnv()
	if want := []string{"/ip4/0.0.0.0/tcp/4001", "/ip6/::/tcp/4001"}; !reflect.DeepEqual(opts.ListenAddrs, want) {
		t.Fatalf("ListenAddrs = %#v, want %#v", opts.ListenAddrs, want)
	}
	if want := []string{"/dns4/discovery.example.com/tcp/4001"}; !reflect.DeepEqual(opts.AnnounceAddrs, want) {
		t.Fatalf("AnnounceAddrs = %#v, want %#v", opts.AnnounceAddrs, want)
	}
	if opts.EnableNATPortMap {
		t.Fatal("EnableNATPortMap = true, want false")
	}
	if opts.EnableRelayService {
		t.Fatal("EnableRelayService = true, want false")
	}
}

func TestRootCommandFlagsOverrideEnv(t *testing.T) {
	t.Setenv(envListenAddrs, "/ip4/0.0.0.0/tcp/4001")
	t.Setenv(envAnnounceAddrs, "")
	t.Setenv(envNATPortMap, "true")
	t.Setenv(envRelayService, "true")

	var got options
	cmd := newRootCommand(func(_ context.Context, opts options, _ io.Writer) error {
		got = opts
		return nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--listen-addr", "/ip4/127.0.0.1/tcp/0",
		"--announce-addr", "/dns4/public.example.com/tcp/4001",
		"--nat-port-map=false",
		"--relay-service=false",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if want := []string{"/ip4/127.0.0.1/tcp/0"}; !reflect.DeepEqual(got.ListenAddrs, want) {
		t.Fatalf("ListenAddrs = %#v, want %#v", got.ListenAddrs, want)
	}
	if want := []string{"/dns4/public.example.com/tcp/4001"}; !reflect.DeepEqual(got.AnnounceAddrs, want) {
		t.Fatalf("AnnounceAddrs = %#v, want %#v", got.AnnounceAddrs, want)
	}
	if got.EnableNATPortMap {
		t.Fatal("EnableNATPortMap = true, want false")
	}
	if got.EnableRelayService {
		t.Fatal("EnableRelayService = true, want false")
	}
}
