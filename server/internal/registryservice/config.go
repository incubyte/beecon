package registryservice

import (
	"fmt"
	"os"
	"strings"
)

// Config is the registry binary's own environment surface (PD59: "the
// registry binary has its own BEECON_REGISTRY_* set — storage location,
// publish token"). It is deliberately separate from the installation's
// internal/config.Config: this binary has no database, no admin key,
// nothing installation-shaped.
type Config struct {
	// StorageDir is where published bundles are persisted (PD60: behind
	// the Store port — driven/diskstore's concrete shape today).
	StorageDir string
	// PublishToken authenticates POST .../bundles (PD60/PD63: publish
	// governance).
	PublishToken string
	// APIKey authenticates GET .../bundles/{version} (PD67: v1 trust is
	// API-key auth + TLS) — the value installations present as
	// BEECON_REGISTRY_API_KEY.
	APIKey string
	// Port is the registry HTTP listener's port; defaults to 8081 when
	// BEECON_REGISTRY_PORT is unset.
	Port string
}

const defaultPort = "8081"

// LoadConfig reads the registry binary's environment, failing fast with a
// clear message when a required value is missing — mirrors
// internal/config.Load's own PD12 fail-fast convention, applied to this
// binary's own small surface.
func LoadConfig() (*Config, error) {
	storageDir := strings.TrimSpace(os.Getenv("BEECON_REGISTRY_STORAGE_DIR"))
	if storageDir == "" {
		return nil, fmt.Errorf("BEECON_REGISTRY_STORAGE_DIR is not set")
	}
	publishToken := strings.TrimSpace(os.Getenv("BEECON_REGISTRY_PUBLISH_TOKEN"))
	if publishToken == "" {
		return nil, fmt.Errorf("BEECON_REGISTRY_PUBLISH_TOKEN is not set")
	}
	apiKey := strings.TrimSpace(os.Getenv("BEECON_REGISTRY_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("BEECON_REGISTRY_API_KEY is not set")
	}
	port := strings.TrimSpace(os.Getenv("BEECON_REGISTRY_PORT"))
	if port == "" {
		port = defaultPort
	}
	return &Config{StorageDir: storageDir, PublishToken: publishToken, APIKey: apiKey, Port: port}, nil
}
