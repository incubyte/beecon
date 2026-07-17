// Command publishembeddedproviders is the Phase 5 registry sub-phase's
// one-time migration script (Slice 6, PD68 AC1): it loads this
// installation's embedded Outlook and Hubspot provider definitions and
// publishes each, over the registry's authenticated publish API, as that
// provider's initial 1.0.0 bundle — minting every tool's immutable tool_ id
// (PD61). It runs once, by hand, against a running registry service
// (cmd/registry); it is not part of that binary (which BOUNDARIES.md
// declares depends on no domain module — this script does, deliberately,
// since turning the embedded seed back into a publishable bundle is
// catalog's own job) and never part of `beecon serve`'s boot path (that
// half of the migration is catalog.Facade.BackfillEmbeddedSeed, called from
// app/wiring.go on every boot).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/registrybundle"
	"beecon/internal/schema"
)

// migratedProviderSlugs are the only embedded providers this one-time
// migration publishes (Slice 6's AC1 names exactly these two — every other
// embedded provider is covered by the boot backfill,
// catalog.Facade.BackfillEmbeddedSeed, not this script).
var migratedProviderSlugs = map[string]bool{"outlook": true, "hubspot": true}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(context.Background()); err != nil {
		logger.Error("publish embedded providers failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	registryURL, publishToken, err := loadEnv()
	if err != nil {
		return err
	}

	definitions, err := catalog.DefaultProviderDefinitions()
	if err != nil {
		return fmt.Errorf("load embedded provider definitions: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	for _, definition := range definitions {
		if !migratedProviderSlugs[definition.Slug] {
			continue
		}
		if err := publishDefinition(ctx, client, registryURL, publishToken, definition); err != nil {
			return fmt.Errorf("publish %s: %w", definition.Slug, err)
		}
	}
	return nil
}

func loadEnv() (registryURL, publishToken string, err error) {
	registryURL = strings.TrimSpace(os.Getenv("BEECON_REGISTRY_URL"))
	if registryURL == "" {
		return "", "", fmt.Errorf("BEECON_REGISTRY_URL is not set")
	}
	publishToken = strings.TrimSpace(os.Getenv("BEECON_REGISTRY_PUBLISH_TOKEN"))
	if publishToken == "" {
		return "", "", fmt.Errorf("BEECON_REGISTRY_PUBLISH_TOKEN is not set")
	}
	return registryURL, publishToken, nil
}

// publishDefinition converts definition into a publishable bundle, generates
// each tool's recorded sample response from its own output schema (the
// embedded seed predates the registry's output-schema-vs-sample gate and
// carries no recorded sample, PD63), and POSTs it to the registry's publish
// endpoint — the same authenticated publish path a catalog maintainer/CI
// uses (PD60/PD63).
func publishDefinition(ctx context.Context, client *http.Client, registryURL, publishToken string, definition catalog.ProviderDefinition) error {
	bundle := catalog.BundleFromProviderDefinition(definition)
	bundle.Tools = withGeneratedSamples(bundle.Tools)

	result, err := postBundle(ctx, client, registryURL, publishToken, definition.Slug, bundle)
	if err != nil {
		return err
	}

	slog.Default().Info("published embedded provider",
		"provider", definition.Slug, "version", result.Version, "toolCount", len(result.Tools))
	return nil
}

func withGeneratedSamples(tools []registrybundle.Tool) []registrybundle.Tool {
	sampled := make([]registrybundle.Tool, len(tools))
	for i, tool := range tools {
		tool.Sample = schema.Example(tool.OutputSchema)
		sampled[i] = tool
	}
	return sampled
}

// publishResponse decodes only the fields this script logs, out of the
// registry's full publish response (registryservice/driving/httpapi's own
// publishResultDTO) — decoded independently rather than importing
// registryservice's DTO type across the installation/registry deployable
// boundary, mirroring driven/registryhttp/client.go's own convention.
type publishResponse struct {
	Version string `json:"version"`
	Tools   []struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"tools"`
}

func postBundle(ctx context.Context, client *http.Client, registryURL, publishToken, providerSlug string, bundle registrybundle.Bundle) (publishResponse, error) {
	body, err := json.Marshal(bundle)
	if err != nil {
		return publishResponse{}, err
	}
	url := fmt.Sprintf("%s/registry/v1/providers/%s/bundles", strings.TrimRight(registryURL, "/"), providerSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return publishResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+publishToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return publishResponse{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return publishResponse{}, err
	}
	if resp.StatusCode != http.StatusCreated {
		return publishResponse{}, fmt.Errorf("registry responded %d: %s", resp.StatusCode, string(respBody))
	}

	var result publishResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return publishResponse{}, err
	}
	return result, nil
}
