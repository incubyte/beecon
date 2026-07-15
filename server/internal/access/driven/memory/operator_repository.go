package memory

import (
	"context"
	"sync"
	"time"

	"beecon/internal/access"
)

// OperatorRepository is an in-memory access.Operators for tests.
type OperatorRepository struct {
	mu        sync.RWMutex
	operators map[access.OperatorID]access.Operator
}

var _ access.Operators = (*OperatorRepository)(nil)

func NewOperatorRepository() *OperatorRepository {
	return &OperatorRepository{operators: map[access.OperatorID]access.Operator{}}
}

func (r *OperatorRepository) Save(_ context.Context, operator access.Operator) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.operators[operator.ID] = operator
	return nil
}

func (r *OperatorRepository) FindByEmail(_ context.Context, email string) (*access.Operator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, operator := range r.operators {
		if operator.Email == email {
			copied := operator
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *OperatorRepository) FindByID(_ context.Context, id access.OperatorID) (*access.Operator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	operator, ok := r.operators[id]
	if !ok {
		return nil, nil
	}
	copied := operator
	return &copied, nil
}

func (r *OperatorRepository) Exists(_ context.Context) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.operators) > 0, nil
}

// ListAll returns every operator account (Slice 4's ListOperators) — no
// defined order is guaranteed here; the facade/handler layer doesn't rely on
// one.
func (r *OperatorRepository) ListAll(_ context.Context) ([]access.Operator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	operators := make([]access.Operator, 0, len(r.operators))
	for _, operator := range r.operators {
		operators = append(operators, operator)
	}
	return operators, nil
}

// UpdatePasswordHash overwrites id's PasswordHash (Slice 4's ChangeMyPassword
// and the break-glass ResetPassword). An unknown id is a no-op, not an
// error — the facade itself already confirmed the operator exists before
// calling this.
func (r *OperatorRepository) UpdatePasswordHash(_ context.Context, id access.OperatorID, passwordHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	operator, ok := r.operators[id]
	if !ok {
		return nil
	}
	operator.PasswordHash = passwordHash
	r.operators[id] = operator
	return nil
}

// SetStatus overwrites id's Status (Slice 4's Deactivate and ResetPassword's
// own reactivation). An unknown id is a no-op, not an error.
func (r *OperatorRepository) SetStatus(_ context.Context, id access.OperatorID, status access.OperatorStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	operator, ok := r.operators[id]
	if !ok {
		return nil
	}
	operator.Status = status
	r.operators[id] = operator
	return nil
}

// CountActive counts operators currently Status == ACTIVE (Deactivate's
// last-active-operator guard, Slice 4 AC6).
func (r *OperatorRepository) CountActive(_ context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, operator := range r.operators {
		if operator.IsActive() {
			count++
		}
	}
	return count, nil
}

// RecordFailedAttempt increments id's FailedAttempts by one (Slice 5's
// brute-force lockout, FD-G) and, only when lockedUntil is non-nil, also
// sets LockedUntil to it. An unknown id is a no-op, not an error.
func (r *OperatorRepository) RecordFailedAttempt(_ context.Context, id access.OperatorID, lockedUntil *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	operator, ok := r.operators[id]
	if !ok {
		return nil
	}
	operator.FailedAttempts++
	if lockedUntil != nil {
		operator.LockedUntil = lockedUntil
	}
	r.operators[id] = operator
	return nil
}

// ResetFailedAttempts zeroes id's FailedAttempts and clears LockedUntil
// (Slice 5's Login, on a successful attempt). An unknown id is a no-op, not
// an error.
func (r *OperatorRepository) ResetFailedAttempts(_ context.Context, id access.OperatorID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	operator, ok := r.operators[id]
	if !ok {
		return nil
	}
	operator.FailedAttempts = 0
	operator.LockedUntil = nil
	r.operators[id] = operator
	return nil
}
