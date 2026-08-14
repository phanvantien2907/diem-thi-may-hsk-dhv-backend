package enum

// WardType enumerates Vietnamese ward categories.
type WardType string

const (
	WardTypeXa     WardType = "xa"
	WardTypePhuong WardType = "phuong"
	WardTypeDacKhu WardType = "dac_khu"
)

// Valid reports whether t is a recognised WardType.
func (t WardType) Valid() bool {
	switch t {
	case WardTypeXa, WardTypePhuong, WardTypeDacKhu:
		return true
	}
	return false
}

// String returns the string representation.
func (t WardType) String() string { return string(t) }
