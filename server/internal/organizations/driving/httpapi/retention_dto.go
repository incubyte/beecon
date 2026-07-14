package httpapi

import "beecon/internal/organizations"

// defaultInstallationRetentionDays is retentionDTO's InstallationDefaultDays
// fallback for a Handler built without WithInstallationDefaultRetentionDays
// (only test constructors that don't care what number appears in that one
// display field) — mirrors config.DefaultRetentionDays. Kept as a local
// constant rather than importing config: this package only ever needs the
// display number, and importing config purely for that one value would
// couple a driving adapter to a value production wiring already resolves
// and hands in explicitly.
const defaultInstallationRetentionDays = 30

// retentionDTO is GET/PUT .../retention's response shape (Slice 7, PD44):
// LogDays/EventDays are null when the org inherits the installation
// default (InstallationDefaultDays names what that default currently is,
// so the console can render "inherit default (N)" without hardcoding N); 0
// means unlimited/disabled for that entity kind.
type retentionDTO struct {
	LogDays                 *int `json:"logDays"`
	EventDays               *int `json:"eventDays"`
	InstallationDefaultDays int  `json:"installationDefaultDays"`
}

func toRetentionDTO(view organizations.RetentionView, installationDefaultDays int) retentionDTO {
	return retentionDTO{
		LogDays:                 view.LogRetentionDays,
		EventDays:               view.EventRetentionDays,
		InstallationDefaultDays: installationDefaultDays,
	}
}

// retentionRequest is PUT .../retention's request body (Slice 7): it
// replaces the org's entire retention record — both fields together,
// mirroring governanceRequest's own null-safe tri-state handling. A field
// absent or JSON null decodes to a nil pointer ("inherit the installation
// default"); 0 decodes to a non-nil pointer to 0 ("unlimited/disabled").
type retentionRequest struct {
	LogDays   *int `json:"logDays"`
	EventDays *int `json:"eventDays"`
}

func (req retentionRequest) toUpdate() organizations.RetentionUpdate {
	return organizations.RetentionUpdate{LogRetentionDays: req.LogDays, EventRetentionDays: req.EventDays}
}
