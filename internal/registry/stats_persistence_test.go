package registry

import (
	"bytes"
	"context"
	"easycr/internal/auth"
	"easycr/internal/state"
	"easycr/internal/testutil"
	"testing"
)

func TestStatisticsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	stateDir, storageDir := t.TempDir(), t.TempDir()
	s, err := state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = auth.EnsureBootstrap(s, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	svc := auth.Service{Store: s}
	_, token, err := svc.CreateToken("stats", []auth.Grant{{Repository: "team/app", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEngine(ctx, storageDir, svc)
	if err != nil {
		t.Fatal(err)
	}
	testutil.PushImage(t, e, "team/app", "latest", token)
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err = NewEngine(ctx, storageDir, auth.Service{Store: s})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	info, err := e.Manifest(ctx, "team/app", "latest")
	if err != nil || info.LastPushedAt == nil || info.SizeBytes == 0 {
		t.Fatalf("after restart: %+v %v", info, err)
	}
}

func TestSizeDeduplicatesSharedBlobsAndUnknownHistory(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	image(t, e, "team/app", "first", "amd64", []byte("shared"))
	image(t, e, "team/app", "second", "arm64", []byte("shared"))
	first, err := e.Manifest(ctx, "team/app", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Manifest(ctx, "team/app", "second")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := e.RepositoryStatistics(ctx, "team/app")
	if err != nil {
		t.Fatal(err)
	}
	if repo.ImageCount != 2 || repo.SizeBytes != first.SizeBytes+second.SizeBytes-int64(len("shared")) {
		t.Fatalf("deduplication: %+v", repo)
	}
	if first.LastPushedAt != nil || first.LastPulledAt != nil || first.PullCount != 0 {
		t.Fatal("invented historical activity")
	}
}
