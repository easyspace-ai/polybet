package httpserver

import (
	"errors"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// isSQLiteForeignKeyViolation reports whether err is an SQLite foreign-key
// constraint failure (modernc.org/sqlite or string form).
func isSQLiteForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	var e *sqlite.Error
	if errors.As(err, &e) {
		switch e.Code() {
		case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
			return true
		case sqlite3.SQLITE_CONSTRAINT:
			msg := strings.ToLower(e.Error())
			return strings.Contains(msg, "foreign key")
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key") &&
		(strings.Contains(msg, "constraint") || strings.Contains(msg, "failed"))
}
