package main

import (
	"context"
	"easycr/internal/auth"
	"easycr/internal/config"
	"easycr/internal/httpapi"
	"easycr/internal/registry"
	"easycr/internal/state"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var listen = net.Listen

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: easycr serve | gc | admin reset-password")
	}
	switch args[0] {
	case "serve":
		c, err := config.Parse(args[1:])
		if err != nil {
			return err
		}
		s, err := state.Open(c.Data)
		if err != nil {
			return err
		}
		defer s.Close()
		// This ordering is intentional: commit the admin hash and emit the single
		// bootstrap event before constructing or opening any HTTP listener.
		if err = auth.EnsureBootstrap(s, out); err != nil {
			return err
		}
		appCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		engine, err := registry.NewEngine(appCtx, filepath.Join(c.Data, "registry"), auth.Service{Store: s})
		if err != nil {
			return err
		}
		defer engine.Close()
		h, err := httpapi.New(c, auth.Service{Store: s}, engine)
		if err != nil {
			return err
		}
		l, err := listen("tcp", c.Listen)
		if err != nil {
			return err
		}
		srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
		done := make(chan error, 1)
		go func() { done <- srv.Serve(l) }()
		select {
		case err = <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			stop, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer closeCancel()
			if err = srv.Shutdown(stop); err != nil {
				_ = srv.Close()
				return err
			}
			err = <-done
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	case "gc", "admin":
		reset := args[0] == "admin"
		rest := args[1:]
		if reset {
			if len(rest) == 0 || rest[0] != "reset-password" {
				return fmt.Errorf("usage: easycr admin reset-password --data DIRECTORY")
			}
			rest = rest[1:]
		}
		f := flag.NewFlagSet(args[0], flag.ContinueOnError)
		f.SetOutput(out)
		dir := f.String("data", "./data", "data directory")
		dry := false
		if !reset {
			f.BoolVar(&dry, "dry-run", false, "preview garbage collection without changes")
		}
		if err := f.Parse(rest); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return fmt.Errorf("unexpected arguments")
		}
		s, err := state.Open(*dir)
		if err != nil {
			return err
		}
		defer s.Close()
		if reset {
			return auth.ResetPassword(s, out)
		}
		e, err := registry.OpenStorage(ctx, filepath.Join(*dir, "registry"))
		if err != nil {
			return err
		}
		return e.GC(ctx, dry)
	default:
		return fmt.Errorf("unknown command; use serve, gc, or admin reset-password")
	}
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "easycr: %v\n", err)
		os.Exit(1)
	}
}
