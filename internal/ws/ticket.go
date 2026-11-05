package ws

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// DefaultTicketTTL is the validity window for a one-time WebSocket handshake ticket.
const DefaultTicketTTL = 30 * time.Second

// TicketEntry stores metadata bound to a single-use WebSocket handshake ticket.
type TicketEntry struct {
	Ticket       string
	UserID       int64
	Role         string
	JWTExpiresAt time.Time
	ExpiresAt    time.Time
}

// TicketStore manages short-lived, single-use handshake tickets in memory
// for the single-instance modular monolith.
type TicketStore struct {
	mu      sync.Mutex
	tickets map[string]TicketEntry
}

// NewTicketStore initializes an in-memory TicketStore.
func NewTicketStore() *TicketStore {
	return &TicketStore{
		tickets: make(map[string]TicketEntry),
	}
}

// IssueTicket generates a cryptographically random 32-byte hex ticket with the given TTL (defaults to 30s).
func (ts *TicketStore) IssueTicket(userID int64, role string, jwtExpiresAt time.Time, ttl time.Duration) (TicketEntry, error) {
	if userID <= 0 {
		return TicketEntry{}, errors.New("ws: invalid user_id for ticket")
	}
	if ttl <= 0 {
		ttl = DefaultTicketTTL
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return TicketEntry{}, err
	}
	ticketStr := hex.EncodeToString(raw)
	now := time.Now().UTC()

	if jwtExpiresAt.IsZero() {
		jwtExpiresAt = now.Add(24 * time.Hour)
	}

	entry := TicketEntry{
		Ticket:       ticketStr,
		UserID:       userID,
		Role:         role,
		JWTExpiresAt: jwtExpiresAt,
		ExpiresAt:    now.Add(ttl),
	}

	ts.mu.Lock()
	ts.sweepExpiredLocked(now)
	ts.tickets[ticketStr] = entry
	ts.mu.Unlock()

	return entry, nil
}

// ConsumeTicket atomically retrieves and deletes a ticket so it can never be reused.
// Returns (entry, true) only if the ticket exists and has not expired.
func (ts *TicketStore) ConsumeTicket(ticket string) (TicketEntry, bool) {
	if ticket == "" {
		return TicketEntry{}, false
	}
	now := time.Now().UTC()

	ts.mu.Lock()
	defer ts.mu.Unlock()

	entry, exists := ts.tickets[ticket]
	if !exists {
		return TicketEntry{}, false
	}
	// Always delete immediately to enforce strict one-time usage.
	delete(ts.tickets, ticket)

	if !now.Before(entry.ExpiresAt) {
		return TicketEntry{}, false
	}
	return entry, true
}

func (ts *TicketStore) sweepExpiredLocked(now time.Time) {
	for k, v := range ts.tickets {
		if !now.Before(v.ExpiresAt) {
			delete(ts.tickets, k)
		}
	}
}

