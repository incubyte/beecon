package httpapi

import (
	"time"

	"beecon/internal/access"
)

// bootstrapRequestDTO is POST /api/v1/operators/bootstrap's request body.
type bootstrapRequestDTO struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// bootstrappedOperatorDTO is Bootstrap's response — never the password or
// its hash.
type bootstrappedOperatorDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func toBootstrappedOperatorDTO(operator access.BootstrappedOperator) bootstrappedOperatorDTO {
	return bootstrappedOperatorDTO{ID: string(operator.ID), Email: operator.Email}
}

// loginRequestDTO is POST /api/v1/auth/login's request body.
type loginRequestDTO struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// operatorProfileDTO is GET /api/v1/auth/me's response.
type operatorProfileDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func toOperatorProfileDTO(profile access.OperatorProfile) operatorProfileDTO {
	return operatorProfileDTO{ID: string(profile.ID), Email: profile.Email}
}

// operatorSummaryDTO is one row of GET /api/v1/operators' response (Slice 4,
// spec Slice 4 AC3): email, status, and created date — never a password
// hash.
type operatorSummaryDTO struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// operatorsListDTO is GET /api/v1/operators' response envelope (API Shape:
// `{ items: [...] }`).
type operatorsListDTO struct {
	Items []operatorSummaryDTO `json:"items"`
}

func toOperatorsListDTO(summaries []access.OperatorSummary) operatorsListDTO {
	items := make([]operatorSummaryDTO, len(summaries))
	for i, summary := range summaries {
		items[i] = operatorSummaryDTO{
			ID:        string(summary.ID),
			Email:     summary.Email,
			Status:    string(summary.Status),
			CreatedAt: summary.CreatedAt,
		}
	}
	return operatorsListDTO{Items: items}
}

// createOperatorRequestDTO is POST /api/v1/operators' request body (Slice 4,
// AC1).
type createOperatorRequestDTO struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// createdOperatorDTO is CreateOperator's response — never the password or
// its hash.
type createdOperatorDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func toCreatedOperatorDTO(created access.CreatedOperator) createdOperatorDTO {
	return createdOperatorDTO{ID: string(created.ID), Email: created.Email}
}

// changeMyPasswordRequestDTO is POST /api/v1/operators/me/password's request
// body (Slice 4, AC4).
type changeMyPasswordRequestDTO struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// resetPasswordRequestDTO is the break-glass POST
// /api/v1/operators/{opId}/reset-password's request body (FD-B): the admin
// supplies the target operator's new password directly.
type resetPasswordRequestDTO struct {
	NewPassword string `json:"newPassword"`
}
