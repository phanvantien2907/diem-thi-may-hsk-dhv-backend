package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServeWS_RejectsRawJWT_InQueryParam(t *testing.T) {
	_, _, dialer := setupTestWSServer(t)
	jwtToken := issueTestJWT(t, 901, "candidate", time.Hour)

	_, resp, err := dialer.Dial("ws://inmemory/ws?token="+jwtToken, nil)
	if err == nil {
		t.Fatal("expected connection with ?token= to be rejected, but it succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized, got: %v", resp)
	}
}

func TestServeWS_AcceptsTicket(t *testing.T) {
	_, router, dialer := setupTestWSServer(t)
	jwtToken := issueTestJWT(t, 902, "candidate", time.Hour)

	// Issue ticket via POST /api/v1/ws/ticket
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ws/ticket", bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/v1/ws/ticket, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Success bool `json:"success"`
		Data    struct {
			Ticket string `json:"ticket"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal ticket error: %v", err)
	}

	// Dial using ticket query param
	conn, resp, err := dialer.Dial("ws://inmemory/ws?ticket="+res.Data.Ticket, nil)
	if err != nil {
		t.Fatalf("expected successful WS handshake with ticket, got: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got: %d", resp.StatusCode)
	}
}

func TestServeWS_RejectsExpiredTicket(t *testing.T) {
	hub, _, dialer := setupTestWSServer(t)

	// Issue a ticket with 5ms TTL and wait for it to expire
	entry, err := hub.TicketStore().IssueTicket(903, "candidate", time.Now().Add(time.Hour), 5*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to issue ticket: %v", err)
	}

	time.Sleep(25 * time.Millisecond)

	_, resp, err := dialer.Dial("ws://inmemory/ws?ticket="+entry.Ticket, nil)
	if err == nil {
		t.Fatal("expected expired ticket to be rejected, but dial succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized for expired ticket, got: %v", resp)
	}
}

func TestServeWS_RejectsReusedTicket(t *testing.T) {
	hub, _, dialer := setupTestWSServer(t)

	entry, err := hub.TicketStore().IssueTicket(904, "candidate", time.Now().Add(time.Hour), 30*time.Second)
	if err != nil {
		t.Fatalf("failed to issue ticket: %v", err)
	}

	// First dial: must succeed
	conn, resp, err := dialer.Dial("ws://inmemory/ws?ticket="+entry.Ticket, nil)
	if err != nil {
		t.Fatalf("first dial failed: %v", err)
	}
	_ = resp
	conn.Close()

	// Second dial with exact same ticket: must fail with 401
	_, replayResp, replayErr := dialer.Dial("ws://inmemory/ws?ticket="+entry.Ticket, nil)
	if replayErr == nil {
		t.Fatal("expected reused ticket to be rejected, but dial succeeded")
	}
	if replayResp == nil || replayResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized on reused ticket, got: %v", replayResp)
	}
}
