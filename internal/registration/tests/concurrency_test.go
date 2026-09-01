package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestConcurrency_BookExamSeat(t *testing.T) {
	if os.Getenv("RUN_CONCURRENCY_TESTS") == "" {
		t.Skip("Skipping concurrency tests; set RUN_CONCURRENCY_TESTS=1 to run")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/hsk_vinh?sslmode=disable"
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(50)

	ctx := context.Background()

	// 0. Cleanup existing dummy data from previous runs
	_, _ = db.Exec("DELETE FROM exam_registrations WHERE exam_registration_exam_session_id IN (SELECT exam_session_id FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'HSK_DUMMY'))")
	_, _ = db.Exec("DELETE FROM exam_seats WHERE exam_seat_exam_session_id IN (SELECT exam_session_id FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'HSK_DUMMY'))")
	_, _ = db.Exec("DELETE FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'HSK_DUMMY')")
	_, _ = db.Exec("DELETE FROM exam_rooms WHERE exam_room_name = 'Room C'")
	_, _ = db.Exec("DELETE FROM exam_types WHERE exam_type_code = 'HSK_DUMMY'")
	_, _ = db.Exec("DELETE FROM candidates WHERE candidate_account_id IN (SELECT account_id FROM accounts WHERE account_username LIKE 'dummy_concurrent_%')")
	_, _ = db.Exec("DELETE FROM accounts WHERE account_username LIKE 'dummy_concurrent_%'")

	// 1. Setup dummy data
	// Create a dummy exam type, room, and session
	var examTypeID, examRoomID, examSessionID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO exam_types (exam_type_code, exam_type_name) VALUES ('HSK_DUMMY', 'Dummy HSK') 
		RETURNING exam_type_id
	`).Scan(&examTypeID)
	if err != nil {
		t.Fatalf("failed to insert exam_type: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO exam_rooms (exam_room_name, exam_room_capacity) VALUES ('Room C', 100) 
		RETURNING exam_room_id
	`).Scan(&examRoomID)
	if err != nil {
		t.Fatalf("failed to insert exam_room: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO exam_sessions (exam_session_exam_type_id, exam_session_exam_room_id, exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee, exam_session_registration_deadline)
		VALUES ($1, $2, now(), 'morning', 100, 500000, now() + interval '7 days')
		RETURNING exam_session_id
	`, examTypeID, examRoomID).Scan(&examSessionID)
	if err != nil {
		t.Fatalf("failed to insert exam_session: %v", err)
	}

	// Insert 100 seats
	for i := 1; i <= 100; i++ {
		_, err = db.ExecContext(ctx, `
			INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status)
			VALUES ($1, $2, 'available')
		`, examSessionID, i)
		if err != nil {
			t.Fatalf("failed to insert seat %d: %v", i, err)
		}
	}

	// 2. Concurrency Test
	numCandidates := 300 // 300 candidates fighting for 100 seats
	candidateIDs := make([]int64, numCandidates)

	for i := 0; i < numCandidates; i++ {
		var accID int64
		err = db.QueryRowContext(ctx, `
			INSERT INTO accounts (account_role_id, account_username, account_password_hash, account_email)
			VALUES (2, $1, 'hash', $2) RETURNING account_id
		`, fmt.Sprintf("dummy_concurrent_%d", i), fmt.Sprintf("dummy%d@test.com", i)).Scan(&accID)
		if err != nil {
			t.Fatalf("failed to create dummy account %d: %v", i, err)
		}

		err = db.QueryRowContext(ctx, `
			INSERT INTO candidates (candidate_account_id, candidate_full_name, candidate_gender, candidate_dob)
			VALUES ($1, $2, 'male', '2000-01-01')
			RETURNING candidate_id
		`, accID, fmt.Sprintf("Candidate %d", i)).Scan(&candidateIDs[i])
		if err != nil {
			t.Fatalf("failed to create candidate %d: %v", i, err)
		}
	}

	t.Logf("Setup complete. Starting concurrency test for 300 candidates on 100 seats...")

	start := time.Now()
	var wg sync.WaitGroup
	var successCount, failCount int32

	for i := 0; i < numCandidates; i++ {
		wg.Add(1)
		go func(cID int64) {
			defer wg.Done()
			var regID int64
			err := db.QueryRow(`SELECT fn_book_exam_seat($1, $2, 15)`, cID, examSessionID).Scan(&regID)
			if err != nil {
				atomic.AddInt32(&failCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(candidateIDs[i])
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("Concurrency test finished in %v", duration)
	t.Logf("Successes: %d (expected 100)", successCount)
	t.Logf("Failures: %d (expected 200)", failCount)
	t.Logf("Average latency: %v per request", duration/time.Duration(numCandidates))

	if successCount != 100 {
		t.Errorf("expected 100 successful bookings, got %d", successCount)
	}

	// 3. Cleanup
	_, _ = db.Exec("DELETE FROM exam_registrations WHERE exam_registration_exam_session_id = $1", examSessionID)
	_, _ = db.Exec("DELETE FROM exam_seats WHERE exam_seat_exam_session_id = $1", examSessionID)
	_, _ = db.Exec("DELETE FROM exam_sessions WHERE exam_session_id = $1", examSessionID)
	_, _ = db.Exec("DELETE FROM exam_rooms WHERE exam_room_id = $1", examRoomID)
	_, _ = db.Exec("DELETE FROM exam_types WHERE exam_type_id = $1", examTypeID)
	_, _ = db.Exec("DELETE FROM candidates WHERE candidate_account_id IN (SELECT account_id FROM accounts WHERE account_username LIKE 'dummy_concurrent_%')")
	_, _ = db.Exec("DELETE FROM accounts WHERE account_username LIKE 'dummy_concurrent_%'")
}
