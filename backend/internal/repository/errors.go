// Package repository implements the data-access layer.
package repository

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
)

var (
	ErrNotFound = errors.New("resource not found")
	ErrConflict = errors.New("resource conflict")
)

// MySQL server error numbers that represent a lost concurrency race:
//
//	1062 ER_DUP_ENTRY         — unique index violated (e.g. double freeze slot)
//	1213 ER_LOCK_DEADLOCK     — transaction chosen as deadlock victim
//	1205 ER_LOCK_WAIT_TIMEOUT — waited too long on a row lock
const (
	mysqlDupEntry        = 1062
	mysqlLockDeadlock    = 1213
	mysqlLockWaitTimeout = 1205
)

// SQLite (used by the WAL-backed concurrency tests) reports writer contention as
// a driver-level error string; matching by message keeps production code free
// of a CGO dependency while still mapping the race loser to a 409.
var sqliteLockMarkers = []string{
	"database is locked",
	"database table is locked",
	"cannot start a transaction within a transaction",
}

// IsLockConflict reports whether err is a database-level concurrency conflict
// (duplicate key, deadlock victim, lock-wait timeout, or a busy single-writer
// database). Callers turn these into an explicit 409 so the loser of a
// concurrent mutation gets a clear "retry" signal instead of a generic 500.
func IsLockConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrConflict) {
		return true
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case mysqlDupEntry, mysqlLockDeadlock, mysqlLockWaitTimeout:
			return true
		}
	}
	msg := err.Error()
	for _, marker := range sqliteLockMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
