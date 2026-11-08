package enum

// CardType mirrors FPT.AI Reader's `type` field.
type CardType string

const (
	CardTypeOld     CardType = "old"      // mặt trước CMND 9 số
	CardTypeOldBack CardType = "old_back" // mặt sau CMND 9 số
	CardTypeNew     CardType = "new"      // mặt trước CMND 12 số / CCCD / Căn cước 2023
	CardTypeNewBack CardType = "new_back" // mặt sau CMND 12 số / CCCD / Căn cước 2023
)

// Valid reports whether c is a recognised CardType.
func (c CardType) Valid() bool {
	switch c {
	case CardTypeOld, CardTypeOldBack, CardTypeNew, CardTypeNewBack:
		return true
	}
	return false
}

// String returns string representation.
func (c CardType) String() string { return string(c) }

// CardTypeDetail mirrors FPT.AI Reader's `type_new` field.
type CardTypeDetail string

const (
	CardDetailCMND09Front      CardTypeDetail = "cmnd_09_front"
	CardDetailCMND12Front      CardTypeDetail = "cmnd_12_front"
	CardDetailCCCD12Front      CardTypeDetail = "cccd_12_front"
	CardDetailOldBack          CardTypeDetail = "old_back"
	CardDetailNewBack          CardTypeDetail = "new_back"
	CardDetailCanCuoc2023Front CardTypeDetail = "can_cuoc_2023_front"
	CardDetailCanCuoc2023Back  CardTypeDetail = "can_cuoc_2023_back"
)

// Valid reports whether d is a recognised CardTypeDetail.
func (d CardTypeDetail) Valid() bool {
	switch d {
	case CardDetailCMND09Front, CardDetailCMND12Front, CardDetailCCCD12Front,
		CardDetailOldBack, CardDetailNewBack,
		CardDetailCanCuoc2023Front, CardDetailCanCuoc2023Back:
		return true
	}
	return false
}

// String returns string representation.
func (d CardTypeDetail) String() string { return string(d) }

// FieldStatus indicates the validation status of a single extracted field.
type FieldStatus string

const (
	StatusOK         FieldStatus = "ok"      // đọc được và qua hết các kiểm tra
	StatusReview     FieldStatus = "review"  // nên để người dùng xem lại
	StatusInvalid    FieldStatus = "invalid" // vi phạm luật cứng (sai độ dài, ngày tương lai...)
	StatusMissing    FieldStatus = "missing" // trường có trên mặt thẻ nhưng không đọc được
	StatusNotPresent FieldStatus = "n/a"     // trường không tồn tại trên loại thẻ/mặt này
)

// Valid reports whether s is a recognised FieldStatus.
func (s FieldStatus) Valid() bool {
	switch s {
	case StatusOK, StatusReview, StatusInvalid, StatusMissing, StatusNotPresent:
		return true
	}
	return false
}

// String returns string representation.
func (s FieldStatus) String() string { return string(s) }

// Decision indicates the card-level conclusion.
type Decision string

const (
	DecisionAccept Decision = "accept" // mọi trường bắt buộc đều ok, có thể tự điền form
	DecisionReview Decision = "review" // điền form nhưng yêu cầu người dùng kiểm tra/sửa
)

// Valid reports whether d is a recognised Decision.
func (d Decision) Valid() bool {
	return d == DecisionAccept || d == DecisionReview
}

// String returns string representation.
func (d Decision) String() string { return string(d) }

// JobStatus tracks asynchronous OCR extraction job status.
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// Valid reports whether j is a recognised JobStatus.
func (j JobStatus) Valid() bool {
	switch j {
	case JobStatusPending, JobStatusProcessing, JobStatusCompleted, JobStatusFailed:
		return true
	}
	return false
}

// String returns string representation.
func (j JobStatus) String() string { return string(j) }
