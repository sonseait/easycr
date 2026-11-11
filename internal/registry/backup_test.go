package registry

import (
	"bytes"
	"context"
	"easycr/internal/auth"
	"easycr/internal/state"
	"easycr/internal/testutil"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupRestore(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	st, err := state.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = auth.EnsureBootstrap(st, &out); err != nil {
		t.Fatal(err)
	}
	password := strings.TrimSpace(strings.Split(out.String(), "password=")[1])
	svc := auth.Service{Store: st}
	e, err := NewEngine(ctx, filepath.Join(source, "registry"), svc)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := svc.CreateToken("backup", []auth.Grant{{Repository: "team/app", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	_, manifest, _ := testutil.PushImage(t, e, "team/app", "v1", secret)
	e.Close()
	st.Close()
	target := t.TempDir()
	if err = filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, relative)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		closeErr := dst.Close()
		if err != nil {
			return err
		}
		return closeErr
	}); err != nil {
		t.Fatal(err)
	}
	st, err = state.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	out.Reset()
	if err = auth.EnsureBootstrap(st, &out); err != nil || out.Len() != 0 {
		t.Fatal("restore bootstrapped again")
	}
	svc = auth.Service{Store: st}
	if _, _, err = svc.Login(password); err != nil {
		t.Fatal("restored admin login failed")
	}
	e, err = NewEngine(ctx, filepath.Join(target, "registry"), svc)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	w := testutil.Request(t, e, "GET", "/v2/team/app/manifests/v1", secret, nil, map[string]string{"Accept": v1.MediaTypeImageManifest})
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), manifest) {
		t.Fatal("restored token or image failed", w.Code)
	}
}
