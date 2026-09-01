package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type UserData struct {
	Token         string `json:"token"`
	ExamSessionID int64  `json:"exam_session_id"`
}

func main() {
	_ = godotenv.Load("../.env")
	dbURL := os.Getenv("DATABASE_URL")
	jwtSecret := os.Getenv("JWT_SECRET")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 1. Cleanup old k6 data
	log.Println("Cleaning up old k6 data...")
	mustExec(db, "DELETE FROM exam_registrations WHERE exam_registration_exam_session_id IN (SELECT exam_session_id FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'K6_TEST'))")
	mustExec(db, "DELETE FROM exam_seats WHERE exam_seat_exam_session_id IN (SELECT exam_session_id FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'K6_TEST'))")
	mustExec(db, "DELETE FROM exam_sessions WHERE exam_session_exam_type_id IN (SELECT exam_type_id FROM exam_types WHERE exam_type_code = 'K6_TEST')")
	mustExec(db, "DELETE FROM exam_rooms WHERE exam_room_name = 'K6 Room'")
	mustExec(db, "DELETE FROM exam_types WHERE exam_type_code = 'K6_TEST'")
	mustExec(db, "DELETE FROM audit_logs WHERE audit_log_actor_account_id IN (SELECT account_id FROM accounts WHERE account_username LIKE 'k6_user_%')")
	mustExec(db, "DELETE FROM candidates WHERE candidate_account_id IN (SELECT account_id FROM accounts WHERE account_username LIKE 'k6_user_%')")
	mustExec(db, "DELETE FROM accounts WHERE account_username LIKE 'k6_user_%'")

	// 2. Setup 1 Session with 5000 seats
	log.Println("Setting up new K6 session with 5,000 seats...")
	var examTypeID, examRoomID, examSessionID int64
	db.QueryRowContext(ctx, "INSERT INTO exam_types (exam_type_code, exam_type_name) VALUES ('K6_TEST', 'K6 Load Test') RETURNING exam_type_id").Scan(&examTypeID)
	db.QueryRowContext(ctx, "INSERT INTO exam_rooms (exam_room_name, exam_room_capacity) VALUES ('K6 Room', 100000) RETURNING exam_room_id").Scan(&examRoomID)
	db.QueryRowContext(ctx, "INSERT INTO exam_sessions (exam_session_exam_type_id, exam_session_exam_room_id, exam_session_date, exam_session_shift, exam_session_capacity, exam_session_fee, exam_session_registration_deadline) VALUES ($1, $2, now(), 'morning', 50000, 500000, now() + interval '7 days') RETURNING exam_session_id", examTypeID, examRoomID).Scan(&examSessionID)

	// Batch insert seats (faster)
	for chunk := 0; chunk < 100; chunk++ { // 100 chunks of 500 = 50,000 seats
		tx, _ := db.BeginTx(ctx, nil)
		for i := 1; i <= 500; i++ {
			seatNum := chunk*500 + i
			tx.ExecContext(ctx, "INSERT INTO exam_seats (exam_seat_exam_session_id, exam_seat_number, exam_seat_status) VALUES ($1, $2, 'available')", examSessionID, seatNum)
		}
		tx.Commit()
	}

	// 3. Setup 100,000 candidates and generate JWTs
	numUsers := 100000
	log.Printf("Generating %d accounts and JWT tokens...\n", numUsers)
	
	users := make([]UserData, 0, numUsers)
	for chunk := 0; chunk < 200; chunk++ { // 200 chunks of 500 = 100,000 users
		tx, _ := db.BeginTx(ctx, nil)
		for i := 1; i <= 500; i++ {
			idx := chunk*500 + i
			var accID int64
			err := tx.QueryRowContext(ctx, "INSERT INTO accounts (account_role_id, account_username, account_password_hash, account_email) VALUES (2, $1, 'hash', $2) RETURNING account_id", fmt.Sprintf("k6_user_%d", idx), fmt.Sprintf("k6user%d@test.com", idx)).Scan(&accID)
			if err != nil {
				log.Fatalf("Error inserting account: %v", err)
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO candidates (candidate_account_id, candidate_full_name, candidate_gender, candidate_dob) VALUES ($1, $2, 'male', '2000-01-01')", accID, fmt.Sprintf("K6 User %d", idx))
			if err != nil {
				log.Fatalf("Error inserting candidate: %v", err)
			}
			
			// Generate JWT
			claims := jwt.MapClaims{
				"sub":       fmt.Sprintf("%d", accID),
				"user_id":   accID,
				"user_role": "candidate",
				"exp":       jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
				"iat":       jwt.NewNumericDate(time.Now()),
			}
			token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
			
			users = append(users, UserData{
				Token:         token,
				ExamSessionID: examSessionID,
			})
		}
		tx.Commit()
		if (chunk+1)*500%2000 == 0 {
			log.Printf("Created %d accounts...\n", (chunk+1)*500)
		}
	}

	// 4. Output to JSON file
	file, _ := os.Create("users.json")
	defer file.Close()
	jsonBytes, _ := json.Marshal(users)
	file.Write(jsonBytes)
	
	log.Println("Done! Wrote users.json")
}

func mustExec(db *sql.DB, query string) {
	if _, err := db.Exec(query); err != nil {
		log.Fatalf("cleanup error on %s: %v", query, err)
	}
}
