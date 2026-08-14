package location

// WardType enumerates Vietnamese ward categories.
type WardType string

const (
	WardTypeXa     WardType = "xa"
	WardTypePhuong WardType = "phuong"
	WardTypeDacKhu WardType = "dac_khu"
)

func (t WardType) Valid() bool {
	switch t {
	case WardTypeXa, WardTypePhuong, WardTypeDacKhu:
		return true
	}
	return false
}
