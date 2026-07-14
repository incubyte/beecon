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

// usersPageDTO is ListUsersByOrg's response: one cursor-paginated page of an
// organization's end-users (Slice 4, PD40), newest first; nextCursor is
// absent when this was the last page — mirrors organizationsPageDTO.
type usersPageDTO struct {
	Items      []userDTO `json:"items"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

func toUsersPageDTO(result organizations.ListUsersResult) usersPageDTO {
	items := make([]userDTO, 0, len(result.Users))
	for _, user := range result.Users {
		items = append(items, toUserDTO(user))
	}
	return usersPageDTO{Items: items, NextCursor: result.NextCursor}
}
