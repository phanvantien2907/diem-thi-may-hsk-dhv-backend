package location

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

