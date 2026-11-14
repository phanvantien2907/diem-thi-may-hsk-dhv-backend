package tests

import (
	"testing"
	"time"

	"github.com/hskvinh/backend/internal/config"
)

func TestLoad_WSDisconnectGraceMS(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/hsk_vinh?sslmode=disable")
	t.Setenv("JWT_SECRET", "synthetic-test-jwt-secret-32-chars!!")
	t.Setenv("CLOUDINARY_URL", "cloudinary://key:secret@cloud")

	// 1. Default when unset -> 3000ms (3s)
	t.Setenv("WS_DISCONNECT_GRACE_MS", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load default failed: %v", err)
	}
	if cfg.WSDisconnectGrace != 3*time.Second {
		t.Fatalf("expected default WSDisconnectGrace=3s, got %v", cfg.WSDisconnectGrace)
	}

	// 2. Explicit 0 -> immediate cancel mode
	t.Setenv("WS_DISCONNECT_GRACE_MS", "0")
	cfgZero, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load zero grace failed: %v", err)
	}
	if cfgZero.WSDisconnectGrace != 0 {
		t.Fatalf("expected WSDisconnectGrace=0, got %v", cfgZero.WSDisconnectGrace)
	}

	// 3. Explicit custom milliseconds
	t.Setenv("WS_DISCONNECT_GRACE_MS", "4500")
	cfgCustom, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load custom grace failed: %v", err)
	}
	if cfgCustom.WSDisconnectGrace != 4500*time.Millisecond {
		t.Fatalf("expected WSDisconnectGrace=4500ms, got %v", cfgCustom.WSDisconnectGrace)
	}

	// 4. Negative or non-numeric value -> fail fast
	for _, bad := range []string{"-100", "abc"} {
		t.Setenv("WS_DISCONNECT_GRACE_MS", bad)
		if _, err := config.Load(); err == nil {
			t.Fatalf("expected config.Load to fail on WS_DISCONNECT_GRACE_MS=%q", bad)
		}
	}
}
