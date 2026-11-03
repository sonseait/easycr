package main

import (
	"bytes"
	"context"
	"easycr/internal/auth"
	"easycr/internal/state"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeBootstrapBeforeListener(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	old := listen
	defer func() { listen = old }()
	calls := 0
	stop := errors.New("test listener stop")
	listen = func(network, addr string) (net.Listener, error) {
		calls++
		if !strings.Contains(out.String(), "bootstrap_admin_password password=") {
			t.Error("listener opened before bootstrap event")
		}
		if _, e := os.Stat(filepath.Join(dir, "metadata.db")); e != nil {
			t.Error("listener before persistence")
		}
		return nil, stop
	}
	if e := run(context.Background(), []string{"serve", "--data", dir}, &out); !errors.Is(e, stop) {
		t.Fatalf("unexpected result: %v", e)
	}
	if calls != 1 {
		t.Fatal("listener not attempted")
	}
	out.Reset()
	listen = func(network, addr string) (net.Listener, error) {
		if out.Len() != 0 {
			t.Error("password logged again")
		}
		return nil, stop
	}
	if e := run(context.Background(), []string{"serve", "--data", dir}, &out); !errors.Is(e, stop) {
		t.Fatal(e)
	}
}
func TestBootstrapFailureDoesNotListen(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "metadata.db"), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	old := listen
	defer func() { listen = old }()
	listen = func(string, string) (net.Listener, error) {
		t.Error("listener called after bootstrap failure")
		return nil, errors.New("unexpected")
	}
	var out bytes.Buffer
	if e := run(context.Background(), []string{"serve", "--data", dir}, &out); e == nil {
		t.Fatal("corrupt DB accepted")
	}
	if out.Len() != 0 {
		t.Fatal("secret emitted on failure")
	}
}
func TestMaintenanceLockAndNoBootstrap(t *testing.T) {
	dir := t.TempDir()
	s, e := state.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"gc", "--data", dir, "--dry-run"}, {"admin", "reset-password", "--data", dir}} {
		if e := run(context.Background(), args, io.Discard); e == nil || !strings.Contains(e.Error(), "data directory is in use") {
			t.Fatal("maintenance not locked", e)
		}
	}
	s.Close()
	var out bytes.Buffer
	if e := run(context.Background(), []string{"gc", "--data", dir, "--dry-run"}, &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "password") {
		t.Fatal("GC bootstrapped admin")
	}
	s, e = state.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e = auth.EnsureBootstrap(s, io.Discard); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if e = run(context.Background(), []string{"admin", "reset-password", "--data", dir}, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "reset_admin_password password=") {
		t.Fatal("reset not logged")
	}
}
