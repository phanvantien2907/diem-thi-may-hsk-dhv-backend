package candidate

// CandidateGender enumerates valid gender values.
type CandidateGender string

const (
	GenderMale   CandidateGender = "male"
	GenderFemale CandidateGender = "female"
	GenderOther  CandidateGender = "other"
)

func (g CandidateGender) Valid() bool {
	switch g {
	case GenderMale, GenderFemale, GenderOther:
		return true
	}
	return false
}

// DocumentType enumerates identity document kinds.
type DocumentType string

const (
	DocumentTypeCCCD     DocumentType = "cccd"
	DocumentTypePassport DocumentType = "passport"
)

func (d DocumentType) Valid() bool {
	return d == DocumentTypeCCCD || d == DocumentTypePassport
}

// DocumentVerificationStatus enumerates document review outcomes.
type DocumentVerificationStatus string

const (
	DocVerificationPending  DocumentVerificationStatus = "pending"
	DocVerificationVerified DocumentVerificationStatus = "verified"
	DocVerificationRejected DocumentVerificationStatus = "rejected"
)

func (s DocumentVerificationStatus) Valid() bool {
	switch s {
	case DocVerificationPending, DocVerificationVerified, DocVerificationRejected:
		return true
	}
	return false
}
