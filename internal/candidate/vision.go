package candidate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // register GIF decoder
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	vision "cloud.google.com/go/vision/apiv1"
	"google.golang.org/api/option"
	visionpb "google.golang.org/genproto/googleapis/cloud/vision/v1"
)

// Portrait rejection reasons (user-facing, Vietnamese).
const (
	ReasonPortraitURLEmpty       = "URL ảnh không được để trống!"
	ReasonPortraitURLInvalid     = "URL ảnh không hợp lệ!"
	ReasonPortraitUnreadable     = "không thể đọc ảnh chân dung, vui lòng tải lại ảnh định dạng JPG hoặc PNG!"
	ReasonPortraitTooLarge       = "dung lượng hoặc kích thước ảnh quá lớn!"
	ReasonPortraitFaceCount      = "phải có chính xác một khuôn mặt trong ảnh!"
	ReasonPortraitEyewear        = "không được đeo kính hoặc trang phục che khuôn mặt!"
	ReasonPortraitBlurred        = "ảnh bị mờ, không rõ nét!"
	ReasonPortraitTooDark        = "ảnh quá tối, không đủ sáng!"
	ReasonPortraitTooBright      = "ảnh quá sáng, bị cháy sáng!"
	ReasonPortraitFaceAngle      = "góc chụp khuôn mặt không hợp lệ (đầu nghiêng quá nhiều)!"
	ReasonPortraitBackground     = "nền ảnh phải màu trắng!"
	ReasonPortraitBackgroundRead = "không thể xác định màu nền ảnh!"
	ReasonPortraitUnsafe         = "ảnh chứa nội dung không phù hợp!"
)

// Validation thresholds.
const (
	maxPortraitBytes  = 10 << 20   // 10 MiB download cap
	maxPortraitPixels = 40_000_000 // decompression-bomb guard
	portraitFetchTTL  = 8 * time.Second

	// A background pixel is "white" when every channel is within whiteTolerance
	// of 255 and the pixel is neutral (max-min channel spread <= whiteMaxChroma).
	whiteTolerance = 40
	whiteMaxChroma = 30
	// Minimum share of sampled border pixels that must be white.
	minWhiteBackgroundRatio = 0.90

	minBrightness = 80.0
	maxBrightness = 240.0

	maxRollAngle = 20.0
	maxPanAngle  = 25.0
	maxTiltAngle = 20.0

	minEyewearScore = 0.60
)

// eyewearTerms are Vision label/object descriptions that indicate glasses.
var eyewearTerms = map[string]struct{}{
	"glasses":     {},
	"eyeglasses":  {},
	"eyewear":     {},
	"sunglasses":  {},
	"goggles":     {},
	"vision care": {},
}

// VisionService defines the interface for portrait image validation.
type VisionService interface {
	// ValidatePortraitImage analyzes a portrait image and returns validation results.
	// A non-nil error means the validation infrastructure failed (network, Vision API),
	// not that the image is invalid; callers should fall back to manual review.
	ValidatePortraitImage(ctx context.Context, imageURL string) (*PortraitValidationResult, error)
	// Close releases resources held by the validator.
	Close() error
}

// PortraitValidationResult contains the analysis results of a portrait image.
type PortraitValidationResult struct {
	// HasFace indicates whether exactly one face was detected.
	HasFace bool
	// HasGlasses indicates whether glasses or headwear were detected.
	HasGlasses bool
	// BackgroundColor is the average color of the sampled background region.
	BackgroundColor color.RGBA
	// WhiteBackgroundRatio is the share of sampled background pixels that are white.
	WhiteBackgroundRatio float64
	// IsWhiteBackground indicates whether the background is white.
	IsWhiteBackground bool
	// IsBlurred indicates whether the image is too blurry.
	IsBlurred bool
	// IsTooDark indicates whether the face region is too dark.
	IsTooDark bool
	// Brightness is the mean luma (0-255) of the face region.
	Brightness float64
	// FaceAngle indicates the head roll angle (rotation around the camera axis).
	FaceAngle float64
	// FacePan indicates the head pan angle (rotation around the vertical axis).
	FacePan float64
	// FaceTilt indicates the head tilt angle (rotation around the horizontal axis).
	FaceTilt float64
	// IsSafe indicates whether the image passes safe search (no explicit content).
	IsSafe bool
	// ManualReviewRequired is true when only the local pixel checks ran because
	// Google Cloud Vision is not configured. Face/glasses rules were not verified.
	ManualReviewRequired bool
	// Valid indicates whether the image passes all executed validation rules.
	Valid bool
	// Reasons lists every violated rule (Vietnamese, user-facing).
	Reasons []string
	// RejectionReason is Reasons joined into one human-readable message.
	RejectionReason string
}

func (r *PortraitValidationResult) reject(reason string) {
	for _, existing := range r.Reasons {
		if existing == reason {
			return
		}
	}
	r.Reasons = append(r.Reasons, reason)
}

func (r *PortraitValidationResult) finalize() *PortraitValidationResult {
	r.Valid = len(r.Reasons) == 0
	r.RejectionReason = strings.Join(r.Reasons, " ")
	return r
}

func rejectedResult(reason string) *PortraitValidationResult {
	r := &PortraitValidationResult{}
	r.reject(reason)
	return r.finalize()
}

// portraitValidator downloads the portrait, runs local pixel checks, and — when a
// Vision client is configured — runs face, eyewear, quality, and safe-search checks.
type portraitValidator struct {
	client     *vision.ImageAnnotatorClient // nil => local checks only
	httpClient *http.Client
}

// NewGoogleVisionService creates a portrait validator backed by Google Cloud Vision.
// The credentialsPath should point to a Google Cloud service account JSON key file.
func NewGoogleVisionService(ctx context.Context, credentialsPath string) (VisionService, error) {
	if credentialsPath == "" {
		return nil, fmt.Errorf("google cloud credentials path is required")
	}
	client, err := vision.NewImageAnnotatorClient(ctx, option.WithCredentialsFile(credentialsPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create vision client: %w", err)
	}
	return &portraitValidator{client: client, httpClient: newPortraitHTTPClient()}, nil
}

// NewLocalPortraitValidator creates a validator that only runs local pixel checks
// (URL, decoding, white background, brightness). It is used when Google Cloud Vision
// is not configured; valid results are flagged ManualReviewRequired.
func NewLocalPortraitValidator() VisionService {
	return &portraitValidator{httpClient: newPortraitHTTPClient()}
}

func newPortraitHTTPClient() *http.Client {
	return &http.Client{
		Timeout: portraitFetchTTL,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !isAllowedPortraitHost(req.URL) {
				return errors.New("portrait redirect not allowed")
			}
			return nil
		},
	}
}

// Close closes the Vision API client.
func (s *portraitValidator) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// ValidatePortraitImage downloads and analyzes a portrait image.
func (s *portraitValidator) ValidatePortraitImage(ctx context.Context, imageURL string) (*PortraitValidationResult, error) {
	u, reason := parsePortraitURL(imageURL)
	if reason != "" {
		return rejectedResult(reason), nil
	}

	data, img, reason, err := s.fetchPortrait(ctx, u)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return rejectedResult(reason), nil
	}

	if s.client != nil {
		ann, err := s.annotate(ctx, data)
		if err == nil {
			return EvaluatePortrait(img, ann), nil
		}
		log.Printf("[vision] Google Vision API call failed (%v); falling back to local image analysis", err)
	}

	return EvaluatePortrait(img, nil), nil
}

// parsePortraitURL only accepts HTTPS Cloudinary delivery URLs. Restricting the host
// prevents the server from being used to fetch arbitrary URLs (SSRF).
func parsePortraitURL(raw string) (*url.URL, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ReasonPortraitURLEmpty
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || !isAllowedPortraitHost(u) {
		return nil, ReasonPortraitURLInvalid
	}
	return u, ""
}

func isAllowedPortraitHost(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "res.cloudinary.com" || strings.HasSuffix(host, ".cloudinary.com")
}

// cloudinaryJPEGURL returns a Cloudinary derived URL that converts the asset to a
// size-limited JPEG, so WebP/HEIC/AVIF uploads can be decoded with the stdlib.
func cloudinaryJPEGURL(u *url.URL) (string, bool) {
	const marker = "/image/upload/"
	idx := strings.Index(u.Path, marker)
	if idx < 0 {
		return "", false
	}
	derived := *u
	derived.Path = u.Path[:idx+len(marker)] + "f_jpg,q_90,w_1600,h_1600,c_limit/" + u.Path[idx+len(marker):]
	derived.RawPath = ""
	return derived.String(), true
}

// fetchPortrait downloads and decodes the portrait. It returns a rejection reason
// for client-side problems (not found, not an image) and an error for transport
// failures that should fall back to manual review.
func (s *portraitValidator) fetchPortrait(ctx context.Context, u *url.URL) ([]byte, image.Image, string, error) {
	candidates := make([]string, 0, 2)
	if derived, ok := cloudinaryJPEGURL(u); ok {
		candidates = append(candidates, derived)
	}
	candidates = append(candidates, u.String())

	var lastErr error
	sawClientError := false
	for _, target := range candidates {
		data, status, err := s.download(ctx, target)
		if err != nil {
			lastErr = err
			continue
		}
		if status != http.StatusOK {
			if status >= 400 && status < 500 {
				sawClientError = true
			} else {
				lastErr = fmt.Errorf("portrait download: unexpected status %d", status)
			}
			continue
		}
		if len(data) > maxPortraitBytes {
			return nil, nil, ReasonPortraitTooLarge, nil
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			sawClientError = true // undecodable format; try the next candidate
			continue
		}
		if cfg.Width*cfg.Height > maxPortraitPixels {
			return nil, nil, ReasonPortraitTooLarge, nil
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			sawClientError = true
			continue
		}
		return data, img, "", nil
	}
	if sawClientError || lastErr == nil {
		return nil, nil, ReasonPortraitUnreadable, nil
	}
	return nil, nil, "", lastErr
}

func (s *portraitValidator) download(ctx context.Context, target string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("portrait download: build request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("portrait download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, resp.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPortraitBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("portrait download: read body: %w", err)
	}
	return data, resp.StatusCode, nil
}

// annotate sends the image bytes (not the URL) to Vision in a single batched call.
func (s *portraitValidator) annotate(ctx context.Context, data []byte) (*visionpb.AnnotateImageResponse, error) {
	ann, err := s.client.AnnotateImage(ctx, &visionpb.AnnotateImageRequest{
		Image: &visionpb.Image{Content: data},
		Features: []*visionpb.Feature{
			// MaxResults > 1 so that multiple faces can actually be detected.
			{Type: visionpb.Feature_FACE_DETECTION, MaxResults: 5},
			{Type: visionpb.Feature_LABEL_DETECTION, MaxResults: 30},
			{Type: visionpb.Feature_OBJECT_LOCALIZATION, MaxResults: 20},
			{Type: visionpb.Feature_SAFE_SEARCH_DETECTION, MaxResults: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to annotate image: %w", err)
	}
	// AnnotateImage reports per-image failures in the response, not as err.
	if ann.GetError() != nil && ann.GetError().GetCode() != 0 {
		return nil, fmt.Errorf("vision annotate error %d: %s", ann.GetError().GetCode(), ann.GetError().GetMessage())
	}
	return ann, nil
}

// EvaluatePortrait applies every portrait rule to a decoded image and the optional
// Vision annotations. When ann is nil only the local pixel rules run and a valid
// result is flagged ManualReviewRequired. All violated rules are collected.
func EvaluatePortrait(img image.Image, ann *visionpb.AnnotateImageResponse) *PortraitValidationResult {
	result := &PortraitValidationResult{IsSafe: true}
	if img == nil || img.Bounds().Empty() {
		return rejectedResult(ReasonPortraitUnreadable)
	}

	// Background (local pixels): sample the top band and upper side bands.
	bg, ratio, ok := analyzeBackground(img)
	result.BackgroundColor = bg
	result.WhiteBackgroundRatio = ratio
	switch {
	case !ok:
		result.reject(ReasonPortraitBackgroundRead)
	case ratio < minWhiteBackgroundRatio:
		result.reject(ReasonPortraitBackground)
	default:
		result.IsWhiteBackground = true
	}

	faceRect := defaultFaceRect(img.Bounds())

	if ann == nil {
		result.ManualReviewRequired = true
		checkBrightness(result, img, faceRect)
		return result.finalize()
	}

	// Face detection.
	if len(ann.GetFaceAnnotations()) != 1 {
		result.reject(ReasonPortraitFaceCount)
	} else {
		result.HasFace = true
		face := ann.GetFaceAnnotations()[0]
		if r, ok := polyRect(face.GetFdBoundingPoly(), img.Bounds()); ok {
			faceRect = r
		} else if r, ok := polyRect(face.GetBoundingPoly(), img.Bounds()); ok {
			faceRect = r
		}

		if atLeast(face.GetHeadwearLikelihood(), visionpb.Likelihood_POSSIBLE) {
			result.HasGlasses = true
			result.reject(ReasonPortraitEyewear)
		}
		if atLeast(face.GetBlurredLikelihood(), visionpb.Likelihood_LIKELY) {
			result.IsBlurred = true
			result.reject(ReasonPortraitBlurred)
		}

		result.FaceAngle = float64(face.GetRollAngle())
		result.FacePan = float64(face.GetPanAngle())
		result.FaceTilt = float64(face.GetTiltAngle())
		if math.Abs(result.FaceAngle) > maxRollAngle ||
			math.Abs(result.FacePan) > maxPanAngle ||
			math.Abs(result.FaceTilt) > maxTiltAngle {
			result.reject(ReasonPortraitFaceAngle)
		}
	}

	// Glasses: Vision faces have no glasses attribute, so use labels and objects.
	if hasEyewear(ann) {
		result.HasGlasses = true
		result.reject(ReasonPortraitEyewear)
	}

	checkBrightness(result, img, faceRect)

	if ss := ann.GetSafeSearchAnnotation(); ss != nil {
		if ss.GetAdult() == visionpb.Likelihood_VERY_LIKELY ||
			ss.GetViolence() == visionpb.Likelihood_VERY_LIKELY ||
			ss.GetMedical() == visionpb.Likelihood_VERY_LIKELY ||
			ss.GetRacy() == visionpb.Likelihood_VERY_LIKELY {
			result.IsSafe = false
			result.reject(ReasonPortraitUnsafe)
		}
	}

	return result.finalize()
}

func atLeast(l, min visionpb.Likelihood) bool {
	return l >= min && l != visionpb.Likelihood_UNKNOWN
}

func hasEyewear(ann *visionpb.AnnotateImageResponse) bool {
	for _, l := range ann.GetLabelAnnotations() {
		if _, ok := eyewearTerms[strings.ToLower(strings.TrimSpace(l.GetDescription()))]; ok && l.GetScore() >= minEyewearScore {
			return true
		}
	}
	for _, o := range ann.GetLocalizedObjectAnnotations() {
		if _, ok := eyewearTerms[strings.ToLower(strings.TrimSpace(o.GetName()))]; ok && o.GetScore() >= minEyewearScore {
			return true
		}
	}
	return false
}

func checkBrightness(result *PortraitValidationResult, img image.Image, faceRect image.Rectangle) {
	result.Brightness = meanLuma(img, faceRect)
	switch {
	case result.Brightness < minBrightness:
		result.IsTooDark = true
		result.reject(ReasonPortraitTooDark)
	case result.Brightness > maxBrightness:
		result.reject(ReasonPortraitTooBright)
	}
}

// analyzeBackground samples the top 5% of the image and the outer 5% columns of the
// upper 25% (beside the head, above the shoulders) — regions that are background in
// a compliant ID portrait. It returns the average color and the white-pixel ratio.
func analyzeBackground(img image.Image) (color.RGBA, float64, bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 10 || h < 10 {
		return color.RGBA{}, 0, false
	}
	topBandH := max(1, h*5/100)
	cornerW := max(1, w*5/100)
	cornerH := h * 25 / 100
	regions := []image.Rectangle{
		image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+topBandH),
		image.Rect(b.Min.X, b.Min.Y+topBandH, b.Min.X+cornerW, b.Min.Y+cornerH),
		image.Rect(b.Max.X-cornerW, b.Min.Y+topBandH, b.Max.X, b.Min.Y+cornerH),
	}
	step := max(1, min(w, h)/200)

	var sumR, sumG, sumB, total, white float64
	for _, r := range regions {
		for y := r.Min.Y; y < r.Max.Y; y += step {
			for x := r.Min.X; x < r.Max.X; x += step {
				cr, cg, cb := rgb8(img.At(x, y))
				sumR += float64(cr)
				sumG += float64(cg)
				sumB += float64(cb)
				total++
				if isWhitePixel(cr, cg, cb) {
					white++
				}
			}
		}
	}
	if total == 0 {
		return color.RGBA{}, 0, false
	}
	avg := color.RGBA{R: uint8(sumR / total), G: uint8(sumG / total), B: uint8(sumB / total), A: 255}
	return avg, white / total, true
}

func isWhitePixel(r, g, b uint8) bool {
	lo := min(r, g, b)
	hi := max(r, g, b)
	return int(lo) >= 255-whiteTolerance && int(hi)-int(lo) <= whiteMaxChroma
}

func meanLuma(img image.Image, r image.Rectangle) float64 {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return 0
	}
	step := max(1, min(r.Dx(), r.Dy())/100)
	var sum, n float64
	for y := r.Min.Y; y < r.Max.Y; y += step {
		for x := r.Min.X; x < r.Max.X; x += step {
			cr, cg, cb := rgb8(img.At(x, y))
			sum += 0.299*float64(cr) + 0.587*float64(cg) + 0.114*float64(cb)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// defaultFaceRect approximates the face area of a centered ID portrait.
func defaultFaceRect(b image.Rectangle) image.Rectangle {
	w, h := b.Dx(), b.Dy()
	return image.Rect(b.Min.X+w*30/100, b.Min.Y+h*20/100, b.Min.X+w*70/100, b.Min.Y+h*60/100)
}

func polyRect(p *visionpb.BoundingPoly, bounds image.Rectangle) (image.Rectangle, bool) {
	vs := p.GetVertices()
	if len(vs) == 0 {
		return image.Rectangle{}, false
	}
	minX, minY := int(vs[0].GetX()), int(vs[0].GetY())
	maxX, maxY := minX, minY
	for _, v := range vs[1:] {
		minX = min(minX, int(v.GetX()))
		minY = min(minY, int(v.GetY()))
		maxX = max(maxX, int(v.GetX()))
		maxY = max(maxY, int(v.GetY()))
	}
	r := image.Rect(minX, minY, maxX, maxY).Intersect(bounds)
	return r, !r.Empty()
}

func rgb8(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}
