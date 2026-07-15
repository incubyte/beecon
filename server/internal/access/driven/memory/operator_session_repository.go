package memory

import (
	"bytes"
	"context"
	"sync"
	"time"

	"beecon/internal/access"
)

// OperatorSessionRepository is an in-memory access.OperatorSessions for
// tests.
type OperatorSessionRepository struct {
	mu       sync.RWMutex
	sessions map[access.OperatorSessionID]access.OperatorSession
}

var _ access.OperatorSessions = (*OperatorSessionRepository)(nil)

func NewOperatorSessionRepository() *OperatorSessionRepository {
	return &OperatorSessionRepository{sessions: map[access.OperatorSessionID]access.OperatorSession{}}
}

func (r *OperatorSessionRepository) Save(_ context.Context, session access.OperatorSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = session
	return nil
}

func (r *OperatorSessionRepository) FindByTokenHash(_ context.Context, tokenHash []byte) (*access.OperatorSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, session := range r.sessions {
		if bytes.Equal(session.TokenHash, tokenHash) {
			copied := session
			return &copied, nil
		}
	}
	return nil, nil
}

// Revoke marks session id's RevokedAt (Slice 2's Logout). Idempotent: an
// unknown id, or one already revoked, is a no-op rather than an error.
func (r *OperatorSessionRepository) Revoke(_ context.Context, id access.OperatorSessionID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[id]
	if !ok || session.RevokedAt != nil {
		return nil
	}
	session.RevokedAt = &at
	r.sessions[id] = session
	return nil
}

// RevokeAllForOperator marks every one of operatorID's still-active sessions
// RevokedAt (Slice 4's deactivate/break-glass reset-password paths).
// Idempotent: an operator with no sessions, or only already-revoked ones, is
// a no-op.
func (r *OperatorSessionRepository) RevokeAllForOperator(_ context.Context, operatorID access.OperatorID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, session := range r.sessions {
		if session.OperatorID != operatorID || session.RevokedAt != nil {
			continue
		}
		session.RevokedAt = &at
		r.sessions[id] = session
	}
	return nil
}

// RevokeAllForOperatorExcept marks every one of operatorID's still-active
// sessions RevokedAt EXCEPT exceptSessionID (Slice 4's ChangeMyPassword,
// carry-forward Slice 2 AC4): the acting session stays alive, every other
// one dies. Idempotent, same as RevokeAllForOperator.
func (r *OperatorSessionRepository) RevokeAllForOperatorExcept(_ context.Context, operatorID access.OperatorID, exceptSessionID access.OperatorSessionID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, session := range r.sessions {
		if session.OperatorID != operatorID || session.RevokedAt != nil || session.ID == exceptSessionID {
			continue
		}
		session.RevokedAt = &at
		r.sessions[id] = session
	}
	return nil
}
