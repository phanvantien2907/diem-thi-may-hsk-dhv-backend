package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hskvinh/backend/internal/middleware"
	"github.com/hskvinh/backend/internal/notification"
	"github.com/hskvinh/backend/internal/ws"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestProcessEvent_ProfileRejected(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	hub := ws.NewHub()
	client := ws.NewClient(hub, nil, 42, "candidate")

	svc := notification.NewService(db)
	svc.SetHub(hub)

	now := time.Now().UTC()
	evt := notification.NewProfileRejectedEvent(42, "Ảnh chân dung bị mờ")

	rows := sqlmock.NewRows([]string{
		"notification_id",
		"notification_account_id",
		"notification_role",
		"notification_type",
		"notification_title",
		"notification_channel",
		"notification_content",
		"notification_is_read",
		"notification_status",
		"notification_is_deleted",
		"notification_deleted_at",
		"notification_created_at",
		"notification_updated_at",
	}).AddRow(
		int64(101),
		int64(42),
		"candidate",
		"PROFILE_REJECTED",
		evt.Title,
		"push",
		evt.Content,
		false,
		"pending",
		false,
		nil,
		now,
		now,
	)

	mock.ExpectQuery(`INSERT INTO notifications`).
		WithArgs(int64(42), "candidate", "PROFILE_REJECTED", evt.Title, evt.Content).
		WillReturnRows(rows)

	svc.ProcessEvent(context.Background(), evt)

	select {
	case msg := <-client.SendChan():
		if !strings.Contains(string(msg), "NOTIFICATION_PAYLOAD") {
			t.Fatalf("expected NOTIFICATION_PAYLOAD on WS client, got: %s", string(msg))
		}
	default:
		t.Fatalf("expected WS client to receive NOTIFICATION_PAYLOAD")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled sql expectations: %v", err)
	}
}

func TestMarkRead_Handler_EmitsMultiTabSyncEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	hub := ws.NewHub()
	tabBClient := ws.NewClient(hub, nil, 42, "candidate")

	svc := notification.NewService(db)
	svc.SetHub(hub)
	h := notification.NewHandler(svc)

	mock.ExpectExec(`UPDATE notifications\s+SET notification_is_read = TRUE`).
		WithArgs(int64(101), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, int64(42))
		c.Next()
	})
	r.PATCH("/api/v1/notifications/:id/read", h.MarkRead)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/101/read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			ID     int64 `json:"id"`
			IsRead bool  `json:"is_read"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success || resp.Data.ID != 101 || !resp.Data.IsRead {
		t.Fatalf("unexpected response payload: %+v", resp)
	}

	// Verify Tab B received the targeted `notification_read` event
	select {
	case wsMsg := <-tabBClient.SendChan():
		if !strings.Contains(string(wsMsg), "notification_read") || !strings.Contains(string(wsMsg), `"id":101`) {
			t.Fatalf("expected notification_read event with id 101, got: %s", string(wsMsg))
		}
	default:
		t.Fatalf("expected Tab B to receive notification_read WS event")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled sql expectations: %v", err)
	}
}

func TestGetMyNotifications_AfterIDSync(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	svc := notification.NewService(db)
	h := notification.NewHandler(svc)

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"notification_id",
		"notification_account_id",
		"notification_role",
		"notification_type",
		"notification_title",
		"notification_channel",
		"notification_content",
		"notification_is_read",
		"notification_status",
		"notification_is_deleted",
		"notification_deleted_at",
		"notification_created_at",
		"notification_updated_at",
	}).AddRow(
		int64(105),
		int64(42),
		"candidate",
		"PROFILE_APPROVED",
		"Hồ sơ đã được phê duyệt",
		"push",
		"Hồ sơ dự thi HSK của bạn đã được duyệt.",
		false,
		"pending",
		false,
		nil,
		now,
		now,
	)

	mock.ExpectQuery(`SELECT .+ FROM notifications WHERE notification_account_id = \$1 AND notification_id > \$2`).
		WithArgs(int64(42), int64(100)).
		WillReturnRows(rows)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, int64(42))
		c.Next()
	})
	r.GET("/api/v1/notifications", h.GetMyNotifications)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications?after_id=100", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled sql expectations: %v", err)
	}
}
