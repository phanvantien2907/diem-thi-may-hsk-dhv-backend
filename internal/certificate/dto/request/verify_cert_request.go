package request

// VerifyCertRequestDTO is the public query for GET /public/certificates/verify.
// Either Code OR (CertificateNo + DOB) must be provided.
type VerifyCertRequestDTO struct {
	Code          string `form:"code"`
	CertificateNo string `form:"certificate_no"`
	DOB           string `form:"dob"` // YYYY-MM-DD — required when certificate_no is used
}
