package main

import (
	"bufio"
	"bytes"
	"context"
	"easycr/internal/state"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func (l *memoryListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *memoryListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *memoryListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5000} }

type eventWriter struct {
	mu sync.Mutex
	bytes.Buffer
}

func (w *eventWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.Write(b)
}
func TestServeHTTPAndGracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	l := &memoryListener{connections: make(chan net.Conn), closed: make(chan struct{})}
	old := listen
	defer func() { listen = old }()
	ready := make(chan struct{})
	var output eventWriter
	listen = func(string, string) (net.Listener, error) {
		output.mu.Lock()
		if !strings.Contains(output.Buffer.String(), "bootstrap_admin_password password=") {
			t.Error("missing pre-listener bootstrap")
		}
		output.mu.Unlock()
		close(ready)
		return l, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"serve", "--data", dir}, &output) }()
	select {
	case <-ready:
	case e := <-done:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	for _, path := range []string{"/healthz", "/", "/app.js", "/style.css", "/v2/"} {
		client, server := net.Pipe()
		select {
		case l.connections <- server:
		case <-time.After(time.Second):
			t.Fatal("accept timed out")
		}
		client.SetDeadline(time.Now().Add(5 * time.Second))
		if _, e := io.WriteString(client, "GET "+path+" HTTP/1.1\r\nHost: localhost:5000\r\nConnection: close\r\n\r\n"); e != nil {
			t.Fatal(e)
		}
		r, e := http.ReadResponse(bufio.NewReader(client), nil)
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(r.Body)
		r.Body.Close()
		client.Close()
		if e != nil {
			t.Fatal(e)
		}
		want := 200
		if path == "/v2/" {
			want = 401
		}
		if r.StatusCode != want {
			t.Fatal(path, r.StatusCode)
		}
		if path == "/" && !bytes.Contains(body, []byte("EasyCR")) {
			t.Fatal("embedded UI missing")
		}
	}
	cancel()
	select {
	case e := <-done:
		if e != nil && !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("graceful shutdown timed out")
	}
	s, e := state.Open(dir)
	if e != nil {
		t.Fatal("DB lock retained after shutdown", e)
	}
	s.Close()
}
