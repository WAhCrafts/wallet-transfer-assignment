package domain

import (
	"time"

	"github.com/google/uuid"
)

// Wallet holds a user's monetary balance.
type Wallet struct {
	ID        uuid.UUID
	Balance   Amount
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewWallet creates a Wallet with a zero balance and a freshly generated UUID v7.
func NewWallet() Wallet {
	now := time.Now().UTC()

	return Wallet{
		ID:        NewID(),
		Balance:   0,
		Version:   0,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
