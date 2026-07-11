package httpapi

import "beecon/internal/organizations"

type createUserRequest struct {
	Name       string `json:"name"`
	ExternalID string `json:"externalId"`
}

type userDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ExternalID string `json:"externalId"`
	CreatedAt  string `json:"createdAt"`
}

func toUserDTO(user organizations.User) userDTO {
	return userDTO{
		ID:         string(user.ID),
		Name:       user.Name,
		ExternalID: user.ExternalID,
		CreatedAt:  user.CreatedAt.Format(rfc3339Millis),
	}
}
