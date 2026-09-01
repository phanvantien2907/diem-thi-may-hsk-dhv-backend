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

func TestConcurrency_TransferRegistration(t *testing.T) {
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

	// 0. Cleanup
	_, _ = db.Exec("DELETE FROM exam_registrations WHERE exam_registration_exam_session_id IN (SELECT exam_session_id FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'HSK_TRANSFER'))")
	_, _ = db.Exec("DELETE FROM exam_seats WHERE exam_seat_exam_session_id IN (SELECT exam_session_id FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'HSK_TRANSFER'))")
	_, _ = db.Exec("DELETE FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'HSK_TRANSFER')")
	_, _ = db.Exec("DELETE FROM exam_rooms WHERE exam_room_name LIKE 'Transfer Room%'")
	_, _ = db.Exec("DELETE FROM exam_types WHERE exam_type_code = 'HSK_TRANSFER'")
	_, _ = db.Exec("DELETE FROM candidates WHERE candidate_account_id IN (SELECT account_id FROM accounts WHERE account_username LIKE 'dummy_transfer_%')")
	_, _ = db.Exec("DELETE FROM accounts WHERE account_username LIKE 'dummy_transfer_%'")

	// 1. Setup
	var examTypeID, roomA_ID, roomB_ID, sessionA_ID, sessionB_ID int64
	db.QueryRowContext(ctx, "INSERT INTO exam_types (exam_type_code, exam_type_name) VALUES ('HSK_TRANSFER', 'Transfer HSK') RETURNING exam_type_id").Scan(&examTypeID)
	db.QueryRowContext(ctx, "INSERT INTO exam_rooms (exam_room_name, exam_room_capacity) VALUES ('Transfer Room A', 100) RETURNING exam_room_id").Scan(&roomA_ID)
	db.QueryRowContext(ctx, "INSERT INTO exam_rooms (exam_room_name, exam_room_capacity) VALUES ('Transfer Room B', 50) RETURNING exam_room_id").Scan(&roomB_ID)

	db.QueryRowContext(ctx, "INSERT INTO exam_sessions (exam_session_exam_type_id, exam_session_exam_room_id, exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee, exam_session_registration_deadline) VALUES ($1, $2, now(), 'morning', 100, 500000, now() + interval '7 days') RETURNING exam_session_id", examTypeID, roomA_ID).Scan(&sessionA_ID)
	db.QueryRowContext(ctx, "INSERT INTO exam_sessions (exam_session_exam_type_id, exam_session_exam_room_id, exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee, exam_session_registration_deadline) VALUES ($1, $2, now(), 'afternoon', 50, 500000, now() + interval '7 days') RETURNING exam_session_id", examTypeID, roomB_ID).Scan(&sessionB_ID)

	for i := 1; i <= 100; i++ {
		db.ExecContext(ctx, "INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status) VALUES ($1, $2, 'available')", sessionA_ID, i)
	}
	for i := 1; i <= 50; i++ {
		db.ExecContext(ctx, "INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status) VALUES ($1, $2, 'available')", sessionB_ID, i)
	}

	numCandidates := 100
	candidateIDs := make([]int64, numCandidates)
	registrationIDs := make([]int64, numCandidates)

	for i := 0; i < numCandidates; i++ {
		var accID int64
		db.QueryRowContext(ctx, "INSERT INTO accounts (account_role_id, account_username, account_password_hash, account_email) VALUES (2, $1, 'hash', $2) RETURNING account_id", fmt.Sprintf("dummy_transfer_%d", i), fmt.Sprintf("dummy_tr%d@test.com", i)).Scan(&accID)
		db.QueryRowContext(ctx, "INSERT INTO candidates (candidate_account_id, candidate_full_name, candidate_gender, candidate_dob) VALUES ($1, $2, 'male', '2000-01-01') RETURNING candidate_id", accID, fmt.Sprintf("Transfer Candidate %d", i)).Scan(&candidateIDs[i])
		db.QueryRowContext(ctx, "SELECT fn_book_exam_seat($1, $2, 15)", candidateIDs[i], sessionA_ID).Scan(&registrationIDs[i])
	}

	t.Logf("Setup complete. 100 candidates registered in Session A. Transferring them all concurrently to Session B (which only has 50 seats)...")

	// 2. Concurrency Test
	start := time.Now()
	var wg sync.WaitGroup
	var successCount, failCount int32

	for i := 0; i < numCandidates; i++ {
		wg.Add(1)
		go func(regID, cID int64) {
			defer wg.Done()
			var newRegID int64
			err := db.QueryRow(`SELECT fn_transfer_registration($1, $2, $3, 15)`, regID, sessionB_ID, cID).Scan(&newRegID)
			if err != nil {
				atomic.AddInt32(&failCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(registrationIDs[i], candidateIDs[i])
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("Concurrency test finished in %v", duration)
	t.Logf("Successes: %d (expected 50)", successCount)
	t.Logf("Failures: %d (expected 50)", failCount)
	t.Logf("Average latency: %v per request", duration/time.Duration(numCandidates))

	if successCount != 50 {
		t.Errorf("expected 50 successful transfers, got %d", successCount)
	}

	// 3. Cleanup
	_, _ = db.Exec("DELETE FROM exam_registrations WHERE exam_registration_exam_session_id IN ($1, $2)", sessionA_ID, sessionB_ID)
	_, _ = db.Exec("DELETE FROM exam_seats WHERE exam_seat_exam_session_id IN ($1, $2)", sessionA_ID, sessionB_ID)
	_, _ = db.Exec("DELETE FROM exam_sessions WHERE exam_session_id IN ($1, $2)", sessionA_ID, sessionB_ID)
	_, _ = db.Exec("DELETE FROM exam_rooms WHERE exam_room_id IN ($1, $2)", roomA_ID, roomB_ID)
	_, _ = db.Exec("DELETE FROM exam_types WHERE exam_type_id = $1", examTypeID)
	_, _ = db.Exec("DELETE FROM candidates WHERE candidate_account_id IN (SELECT account_id FROM accounts WHERE account_username LIKE 'dummy_transfer_%')")
	_, _ = db.Exec("DELETE FROM accounts WHERE account_username LIKE 'dummy_transfer_%'")
}
