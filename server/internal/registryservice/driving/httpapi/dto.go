package httpapi

import (
	"time"

	"beecon/internal/registryservice"
)

// toolIdentityDTO is one tool's registry-minted identity (Slice 1's publish
// response, PD61): the immutable tool_ id alongside its slug.
type toolIdentityDTO struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// bundleDiffDTO names the tool/trigger slugs a publish added or removed
// relative to the provider's previous version (Slice 2's last AC).
type bundleDiffDTO struct {
	Tools    []string `json:"tools"`
	Triggers []string `json:"triggers"`
}

// publishResultDTO is Publish's response shape.
type publishResultDTO struct {
	Version     string            `json:"version"`
	ContentHash string            `json:"contentHash"`
	Tools       []toolIdentityDTO `json:"tools"`
	Added       bundleDiffDTO     `json:"added"`
	Removed     bundleDiffDTO     `json:"removed"`
}

// bundleVersionDTO is one row of ListVersions' response (Slice 3): a
// version this provider has published, its content hash, and when it was
// published.
type bundleVersionDTO struct {
	Version     string `json:"version"`
	ContentHash string `json:"contentHash"`
	PublishedAt string `json:"publishedAt"`
}

// bundleVersionsDTO is ListVersions' response envelope.
type bundleVersionsDTO struct {
	Items []bundleVersionDTO `json:"items"`
}

func toBundleVersionsDTO(versions []registryservice.BundleVersionSummary) bundleVersionsDTO {
	items := make([]bundleVersionDTO, 0, len(versions))
	for _, v := range versions {
		items = append(items, bundleVersionDTO{
			Version:     v.Version,
			ContentHash: v.ContentHash,
			PublishedAt: v.PublishedAt.Format(time.RFC3339),
		})
	}
	return bundleVersionsDTO{Items: items}
}

func toPublishResultDTO(result registryservice.PublishResult) publishResultDTO {
	tools := make([]toolIdentityDTO, 0, len(result.Tools))
	for _, tool := range result.Tools {
		tools = append(tools, toolIdentityDTO{ID: tool.ID, Slug: tool.Slug})
	}
	return publishResultDTO{
		Version:     result.Version,
		ContentHash: result.ContentHash,
		Tools:       tools,
		Added:       bundleDiffDTO{Tools: result.Added.Tools, Triggers: result.Added.Triggers},
		Removed:     bundleDiffDTO{Tools: result.Removed.Tools, Triggers: result.Removed.Triggers},
	}
}
