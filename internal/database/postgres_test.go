package database

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

func TestCheckPoolAvailability(t *testing.T) {
	// Nil db check
	assert.NoError(t, CheckPoolAvailability(nil, 0.85))

	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	// Default threshold when 0 is passed
	err = CheckPoolAvailability(db, 0)
	assert.NoError(t, err) // When MaxOpenConns = 0 (unlimited), never exhausts

	db.SetMaxOpenConns(10)

	// Since sqlmock has 0 in-use connections, 0/10 < 0.85 -> No error
	err = CheckPoolAvailability(db, 0.85)
	assert.NoError(t, err)
}

func TestCheckPoolAvailability_ThresholdLogic(t *testing.T) {
	// Verify ratio calculation helper logic
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	db.SetMaxOpenConns(1)

	// When threshold is valid and no connections are in use
	assert.NoError(t, CheckPoolAvailability(db, 0.50))
}
