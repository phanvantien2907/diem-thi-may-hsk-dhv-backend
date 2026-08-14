package enum

// CandidateGender enumerates valid gender values.
type CandidateGender string

const (
	GenderMale   CandidateGender = "male"
	GenderFemale CandidateGender = "female"
	GenderOther  CandidateGender = "other"
)

// Valid reports whether g is a recognised CandidateGender.
func (g CandidateGender) Valid() bool {
	switch g {
	case GenderMale, GenderFemale, GenderOther:
		return true
	}
	return false
}

// String returns the string representation.
func (g CandidateGender) String() string { return string(g) }

// DocumentType enumerates identity document kinds.
type DocumentType string

const (
	DocumentTypeCCCD     DocumentType = "cccd"
	DocumentTypePassport DocumentType = "passport"
)

// Valid reports whether d is a recognised DocumentType.
func (d DocumentType) Valid() bool {
	return d == DocumentTypeCCCD || d == DocumentTypePassport
}

// String returns the string representation.
func (d DocumentType) String() string { return string(d) }

// DocumentVerificationStatus enumerates document review outcomes.
type DocumentVerificationStatus string

const (
	DocVerificationPending  DocumentVerificationStatus = "pending"
	DocVerificationVerified DocumentVerificationStatus = "verified"
	DocVerificationRejected DocumentVerificationStatus = "rejected"
)

// Valid reports whether s is a recognised DocumentVerificationStatus.
func (s DocumentVerificationStatus) Valid() bool {
	switch s {
	case DocVerificationPending, DocVerificationVerified, DocVerificationRejected:
		return true
	}
	return false
}

// String returns the string representation.
func (s DocumentVerificationStatus) String() string { return string(s) }
