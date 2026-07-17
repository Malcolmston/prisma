package prisma

import (
	"database/sql"
	"errors"
	"strings"
)

// Prisma-style error codes surfaced by [Error.Code]. Only the subset that this
// driver-agnostic library can reliably detect from database/sql errors is
// mapped; the codes mirror Prisma's client error reference so callers familiar
// with Prisma can branch on the same values.
const (
	// CodeUniqueViolation (P2002) is a unique-constraint violation.
	CodeUniqueViolation = "P2002"
	// CodeForeignKeyViolation (P2003) is a foreign-key-constraint violation.
	CodeForeignKeyViolation = "P2003"
	// CodeNotNullViolation (P2011) is a NOT NULL constraint violation.
	CodeNotNullViolation = "P2011"
	// CodeValueTooLong (P2000) means a value exceeded the column's length.
	CodeValueTooLong = "P2000"
	// CodeRecordNotFound (P2025) means a required record was not found.
	CodeRecordNotFound = "P2025"
)

// Error is a typed database error with a Prisma-style [Error.Code]. Execution
// methods wrap driver errors in an *Error so callers can branch on the code
// (via [errors.As]) or match a sentinel (via [errors.Is]) instead of parsing
// vendor-specific message strings.
type Error struct {
	// Code is the Prisma-style error code (e.g. "P2002").
	Code string
	// Message is a human-readable description.
	Message string
	// Cause is the underlying driver error, if any.
	Cause error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return "prisma: " + e.Code + ": " + e.Message + ": " + e.Cause.Error()
	}
	return "prisma: " + e.Code + ": " + e.Message
}

// Unwrap returns the underlying driver error so [errors.Is] and [errors.As] can
// traverse to it.
func (e *Error) Unwrap() error { return e.Cause }

// Is reports whether target is an *Error with the same Code, which lets the
// package's sentinel errors ([ErrUniqueViolation] and friends) match any
// wrapped error carrying that code.
func (e *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return t.Code == e.Code
	}
	return false
}

// Sentinel errors for matching a mapped [Error] by code with [errors.Is].
var (
	// ErrUniqueViolation matches any P2002 unique-constraint error.
	ErrUniqueViolation = &Error{Code: CodeUniqueViolation, Message: "unique constraint violation"}
	// ErrForeignKeyViolation matches any P2003 foreign-key error.
	ErrForeignKeyViolation = &Error{Code: CodeForeignKeyViolation, Message: "foreign key constraint violation"}
	// ErrNotNullViolation matches any P2011 NOT NULL error.
	ErrNotNullViolation = &Error{Code: CodeNotNullViolation, Message: "null constraint violation"}
	// ErrValueTooLong matches any P2000 value-too-long error.
	ErrValueTooLong = &Error{Code: CodeValueTooLong, Message: "value too long for column"}
	// ErrRecordNotFound matches any P2025 record-not-found error.
	ErrRecordNotFound = &Error{Code: CodeRecordNotFound, Message: "record not found"}
)

// mapError translates a raw driver error into a typed [Error] when its message
// matches a known constraint pattern (across SQLite, PostgreSQL and MySQL
// phrasings). sql.ErrNoRows maps to P2025. Unrecognized errors are returned
// unchanged.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return &Error{Code: CodeRecordNotFound, Message: "record not found", Cause: err}
	}
	msg := strings.ToLower(err.Error())
	switch {
	case containsAny(msg, "unique constraint", "duplicate key", "duplicate entry", "unique violation"):
		return &Error{Code: CodeUniqueViolation, Message: "unique constraint violation", Cause: err}
	case containsAny(msg, "foreign key constraint", "foreign key violation", "violates foreign key"):
		return &Error{Code: CodeForeignKeyViolation, Message: "foreign key constraint violation", Cause: err}
	case containsAny(msg, "not null constraint", "null value", "cannot be null", "not-null"):
		return &Error{Code: CodeNotNullViolation, Message: "null constraint violation", Cause: err}
	case containsAny(msg, "value too long", "data too long", "string or binary data would be truncated"):
		return &Error{Code: CodeValueTooLong, Message: "value too long for column", Cause: err}
	}
	return err
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
