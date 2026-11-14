package location

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/location/dto/request"
	"github.com/hskvinh/backend/internal/location/matcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeospatialSync_FallbackToLocalSnapshotWhenAPIDown(t *testing.T) {
	// Spin up an HTTP server that always fails with 503 Service Unavailable
	failingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
	}))
	defer failingSrv.Close()

	svc := NewService(nil, nil)
	res, err := svc.SyncGeospatialDataWithOptions(context.Background(), GeospatialSyncOptions{
		V1URL:          failingSrv.URL + "/api/?depth=3",
		V2ProvincesURL: failingSrv.URL + "/api/v2/p/?depth=3",
		V2WardsURL:     failingSrv.URL + "/api/v2/w/",
		HTTPClient:     &http.Client{Timeout: 2 * time.Second},
		MaxAttempts:    2,
		RetryBaseDelay: 2 * time.Millisecond,
	})

	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "snapshot_fallback", res.Source)
	assert.Equal(t, 63, res.V1Provinces)
	assert.Equal(t, 696, res.Districts)
	assert.Equal(t, 10051, res.V1Wards)
	assert.Equal(t, 34, res.V2Provinces)
	assert.Equal(t, 3321, res.V2Wards)

	// Confirm the global resolver is populated cleanly after fallback
	resolved := svc.ResolveAddress(context.Background(), request.ResolveAddressRequestDTO{
		Address: "Xã Nam Lộc, Huyện Nam Đàn, Nghệ An",
	})
	assert.True(t, resolved.Matched)
	assert.Equal(t, "Xã Nam Đàn", resolved.WardName)
	assert.True(t, resolved.IsLegacyAddress)
}

func TestResolveAddressHandler_ValidAndEmptyRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(nil, nil)
	h := NewHandler(svc)

	router := gin.New()
	router.POST("/api/v1/locations/resolve-address", h.ResolveAddress)

	// 1. Valid request with legacy 9-digit CMND address
	body, _ := json.Marshal(request.ResolveAddressRequestDTO{
		Address: "Xã Nam Lộc, Huyện Nam Đàn, Nghệ An",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/locations/resolve-address", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var okResp struct {
		Data matcher.ResolvedAddress `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &okResp))
	assert.True(t, okResp.Data.Matched)
	assert.Equal(t, "Xã Nam Đàn", okResp.Data.WardName)
	assert.True(t, okResp.Data.IsLegacyAddress)

	// 2. Empty payload should return 400 Bad Request
	emptyReq := httptest.NewRequest(http.MethodPost, "/api/v1/locations/resolve-address", bytes.NewBufferString(`{}`))
	emptyReq.Header.Set("Content-Type", "application/json")
	wEmpty := httptest.NewRecorder()
	router.ServeHTTP(wEmpty, emptyReq)

	assert.Equal(t, http.StatusBadRequest, wEmpty.Code)
}

// H-09: Verify (1) public /locations/clear-cache is not exposed without Admin JWT,
// (2) oversized /locations/resolve-address payloads (>512 chars) are rejected with 400,
// and (3) OpenAPI returning HTTP 200 with empty [] does not wipe out DefaultResolver.
func TestLocation_H09_SecurityBoundsAndEmptySyncGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := NewService(nil, nil)
	h := NewHandler(svc)

	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterRoutes(v1, h, "test-jwt-secret-32-bytes-minimum!")

	// 1. Unauthenticated POST /api/v1/locations/clear-cache must NOT succeed with 200 OK
	clearReq := httptest.NewRequest(http.MethodPost, "/api/v1/locations/clear-cache", nil)
	wClear := httptest.NewRecorder()
	router.ServeHTTP(wClear, clearReq)
	assert.NotEqual(t, http.StatusOK, wClear.Code, "public /locations/clear-cache should not be accessible without admin auth")

	// 2. Oversized address input (10 KB) on POST /api/v1/locations/resolve-address must return 400 Bad Request
	hugeAddr, _ := json.Marshal(request.ResolveAddressRequestDTO{
		Address: strings.Repeat("A", 10000),
	})
	hugeReq := httptest.NewRequest(http.MethodPost, "/api/v1/locations/resolve-address", bytes.NewReader(hugeAddr))
	hugeReq.Header.Set("Content-Type", "application/json")
	wHuge := httptest.NewRecorder()
	router.ServeHTTP(wHuge, hugeReq)
	assert.Equal(t, http.StatusBadRequest, wHuge.Code, "oversized address input must be rejected with 400 Bad Request")

	// 3. Upstream returning HTTP 200 with empty JSON array `[]` must fallback to embedded snapshot instead of wiping DefaultResolver
	emptyRoundTripper := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		rec.WriteHeader(http.StatusOK)
		_, _ = rec.WriteString(`[]`)
		return rec.Result(), nil
	})
	res, err := svc.SyncGeospatialDataWithOptions(context.Background(), GeospatialSyncOptions{
		V1URL:          "http://empty.local/api/?depth=3",
		V2ProvincesURL: "http://empty.local/api/v2/p/?depth=3",
		V2WardsURL:     "http://empty.local/api/v2/w/",
		HTTPClient:     &http.Client{Transport: emptyRoundTripper},
		MaxAttempts:    1,
	})
	require.NoError(t, err)
	assert.Equal(t, "snapshot_fallback", res.Source, "empty [] upstream payload must fallback to embedded snapshot")
	assert.GreaterOrEqual(t, res.V2Provinces, MinExpectedProvinces)
	assert.GreaterOrEqual(t, res.V2Wards, MinExpectedSyncedWards)
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
