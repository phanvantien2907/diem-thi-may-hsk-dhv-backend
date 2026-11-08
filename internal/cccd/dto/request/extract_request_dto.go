package request

// ExtractQueryDTO binds query parameters for POST /api/v1/cccd/extract.
type ExtractQueryDTO struct {
	// Sync when true indicates synchronous processing fallback (blocks until complete).
	// Default is false (async 202 Accepted + WebSocket notification).
	Sync bool `form:"sync"`
}
