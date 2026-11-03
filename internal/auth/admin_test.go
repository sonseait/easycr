package auth

import (
	"bytes"
	"easycr/internal/state"
	"strings"
	"testing"
)

func TestBootstrapOnce(t *testing.T) {
	dir := t.TempDir()
	s, err := state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	if err = EnsureBootstrap(s, &logs); err != nil {
		t.Fatal(err)
	}
	if strings.Count(logs.String(), "bootstrap_admin_password") != 1 {
		t.Fatal("missing bootstrap event")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = EnsureBootstrap(s, &logs); err != nil {
		t.Fatal(err)
	}
	if strings.Count(logs.String(), "bootstrap_admin_password") != 1 {
		t.Fatal("bootstrap repeated")
	}
}
