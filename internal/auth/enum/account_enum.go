package enum

// AccountStatus enumerates allowed account lifecycle states.
type AccountStatus string

const (
	AccountStatusActive      AccountStatus = "active"
	AccountStatusLocked      AccountStatus = "locked"
	AccountStatusPending     AccountStatus = "pending"
	AccountStatusDisabled    AccountStatus = "disabled"
	AccountStatusDeactivated AccountStatus = "deactivated"
)

// Valid reports whether s is a recognised AccountStatus.
func (s AccountStatus) Valid() bool {
	switch s {
	case AccountStatusActive, AccountStatusLocked, AccountStatusPending, AccountStatusDisabled, AccountStatusDeactivated:
		return true
	}
	return false
}

// String returns the string representation of AccountStatus.
func (s AccountStatus) String() string { return string(s) }
