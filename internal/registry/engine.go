package registry

import (
	"context"
	"net/http"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry/handlers"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
)

// NewEngine provides an unauthenticated engine for integration testing only.
// It must not be exposed by a production server without an access controller.
func NewEngine(dir string) (http.Handler, error) {
	cfg := &configuration.Configuration{Storage: configuration.Storage{"filesystem": configuration.Parameters{"rootdirectory": dir}, "maintenance": configuration.Parameters{"uploadpurging": map[any]any{"enabled": false}}}}
	cfg.HTTP.Secret = "test-only-not-for-deployment"
	return handlers.NewApp(context.Background(), cfg), nil
}
