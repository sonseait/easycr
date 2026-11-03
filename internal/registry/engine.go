package registry

import (
	"context"
	"crypto/rand"
	"easycr/internal/auth"
	"encoding/base64"
	"fmt"
	"github.com/distribution/distribution/v3"
	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry/handlers"
	"github.com/distribution/distribution/v3/registry/storage"
	"github.com/distribution/distribution/v3/registry/storage/driver"
	"github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	"net/http"
	"sync"
)

// Engine exposes only an authenticated registry. Admin storage operations share
// the mutation lock with protocol writes so retag conflict checks are atomic.
type Engine struct {
	Handler   http.Handler
	Namespace distribution.Namespace
	Driver    driver.StorageDriver
	mu        sync.RWMutex
}

func NewEngine(ctx context.Context, dir string, service auth.Service) (*Engine, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("token service required")
	}
	e, err := OpenStorage(ctx, dir)
	if err != nil {
		return nil, err
	}
	cfg := &configuration.Configuration{Storage: configuration.Storage{"filesystem": configuration.Parameters{"rootdirectory": dir}, "delete": configuration.Parameters{"enabled": true}, "maintenance": configuration.Parameters{"uploadpurging": map[any]any{"enabled": false}}}, Auth: configuration.Auth{"easycr": configuration.Parameters{"service": service}}}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	cfg.HTTP.Secret = base64.RawURLEncoding.EncodeToString(secret)
	cfg.Log.AccessLog.Disabled = true
	e.Handler = handlers.NewApp(ctx, cfg)
	return e, nil
}
func OpenStorage(ctx context.Context, dir string) (*Engine, error) {
	d, err := filesystem.FromParameters(map[string]any{"rootdirectory": dir})
	if err != nil {
		return nil, err
	}
	ns, err := storage.NewRegistry(ctx, d, storage.EnableDelete)
	if err != nil {
		return nil, err
	}
	return &Engine{Namespace: ns, Driver: d}, nil
}
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" || r.Method == "HEAD" {
		e.mu.RLock()
		defer e.mu.RUnlock()
	} else {
		e.mu.Lock()
		defer e.mu.Unlock()
	}
	e.Handler.ServeHTTP(w, r)
}

func (e *Engine) Close() error {
	if app, ok := e.Handler.(*handlers.App); ok {
		return app.Shutdown()
	}
	return nil
}
