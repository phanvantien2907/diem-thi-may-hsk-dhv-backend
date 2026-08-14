package auth

// AccountStatus enumerates allowed account lifecycle states.
type AccountStatus string

const (
	AccountStatusActive  AccountStatus = "active"
	AccountStatusLocked  AccountStatus = "locked"
	AccountStatusPending AccountStatus = "pending"
)

// Valid reports whether s is a recognised AccountStatus.
func (s AccountStatus) Valid() bool {
	switch s {
	case AccountStatusActive, AccountStatusLocked, AccountStatusPending:
		return true
	}
	return false
}
