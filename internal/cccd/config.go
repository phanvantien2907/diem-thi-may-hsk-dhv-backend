package cccd

// Config holds operational and image validation parameters for the CCCD OCR/AI service.
type Config struct {
	// Gemini API settings
	GeminiAPIKey     string
	GeminiModel      string
	GeminiTimeoutSec float64
	GeminiMaxRetries int

	// Image validation thresholds (compatible with FPT.AI Reader recommendations)
	MaxUploadBytes int
	MinImageWidth  int
	MinImageHeight int
	MaxImageEdge   int
	MinSharpness   float64
	MinBrightness  float64
	MaxBrightness  float64

	// Pipeline execution settings
	MaxConcurrency       int
	SecondPassOnConflict bool
}

// DefaultConfig returns the standard production configuration matching Python's Settings.
func DefaultConfig() Config {
	return Config{
		GeminiModel:          DefaultGeminiVisionModel,
		GeminiTimeoutSec:     45.0,
		GeminiMaxRetries:     2,
		MaxUploadBytes:       5 * 1024 * 1024, // 5 MB
		MinImageWidth:        640,
		MinImageHeight:       480,
		MaxImageEdge:         1600,
		MinSharpness:         20.0,
		MinBrightness:        35.0,
		MaxBrightness:        225.0,
		MaxConcurrency:       4,
		SecondPassOnConflict: true,
	}
}
