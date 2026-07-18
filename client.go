package prisma

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
)

// ErrNotFound is returned by FindUnique and FindFirst when no row matches. It
// wraps the P2025 "record not found" code (see [Error]).
var ErrNotFound = errors.New("prisma: record not found")

// execer is the subset of *sql.DB and *sql.Tx used to run queries, letting the
// same builder execute against either a connection pool or a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Client is the entry point for building and executing queries. It bundles a
// *sql.DB, a model Registry and a Dialect. A Client with a nil DB can still
// build SQL (via the *SQL methods) but cannot execute.
type Client struct {
	db       *sql.DB
	registry *Registry
	dialect  Dialect
}

// Option configures a Client.
type Option func(*Client)

// WithDialect sets the SQL dialect (default: Question).
func WithDialect(d Dialect) Option {
	return func(c *Client) { c.dialect = d }
}

// WithRegistry uses an existing registry instead of a fresh one.
func WithRegistry(r *Registry) Option {
	return func(c *Client) { c.registry = r }
}

// NewClient constructs a Client around db (which may be nil for SQL-only use).
func NewClient(db *sql.DB, opts ...Option) *Client {
	c := &Client{db: db, registry: NewRegistry(), dialect: Question}
	for _, o := range opts {
		o(c)
	}
	if c.registry == nil {
		c.registry = NewRegistry()
	}
	return c
}

// Registry returns the client's model registry.
func (c *Client) Registry() *Registry { return c.registry }

// DB returns the underlying *sql.DB (may be nil).
func (c *Client) DB() *sql.DB { return c.db }

// Register compiles and stores the schema for v on this client's registry.
func (c *Client) Register(v any) (*Model, error) {
	return c.registry.Register(v)
}

// NewQuery starts a new query for the model T. If T is not yet registered on
// the client it is registered lazily; any registration error is deferred and
// surfaced by the terminal method.
func NewQuery[T any](c *Client) *Query[T] {
	return newQuery[T](c, c.db)
}

// NewTxQuery starts a new query for model T that executes against the
// transaction tx instead of the client's connection pool. It is otherwise
// identical to [NewQuery].
func NewTxQuery[T any](tx *Tx) *Query[T] {
	return newQuery[T](tx.client, tx.tx)
}

// newQuery is the shared constructor behind NewQuery and NewTxQuery; conn is the
// executor (a *sql.DB or *sql.Tx) that terminal methods run against.
func newQuery[T any](c *Client, conn execer) *Query[T] {
	var zero T
	t := reflect.TypeOf(zero)
	q := &Query[T]{client: c, conn: conn}
	if t == nil {
		q.err = fmt.Errorf("prisma: NewQuery[T] requires a concrete struct type")
		return q
	}
	m, ok := c.registry.ModelOf(t)
	if !ok {
		// Attempt lazy registration so callers need not pre-register.
		var err error
		m, err = c.registry.Register(zero)
		if err != nil {
			q.err = err
			return q
		}
	}
	q.model = m
	return q
}

// executor returns the connection the query runs against, or an error if none
// was configured (a client built with a nil *sql.DB).
func (q *Query[T]) executor() (execer, error) {
	if isNilExecer(q.conn) {
		return nil, fmt.Errorf("prisma: no *sql.DB configured")
	}
	return q.conn, nil
}

// isNilExecer reports whether an execer interface is nil or holds a nil pointer.
func isNilExecer(e execer) bool {
	if e == nil {
		return true
	}
	if db, ok := e.(*sql.DB); ok {
		return db == nil
	}
	if tx, ok := e.(*sql.Tx); ok {
		return tx == nil
	}
	return false
}

// FindMany executes the SELECT and scans every row into a slice of T.
func (q *Query[T]) FindMany(ctx context.Context) ([]T, error) {
	w := q.newWriter()
	sqlStr, plan, err := q.buildSelect(w)
	if err != nil {
		return nil, err
	}
	conn, err := q.executor()
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, sqlStr, w.args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []T
	for rows.Next() {
		var rec T
		dest := scanDest(reflect.ValueOf(&rec).Elem(), plan)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if q.distAll || len(q.distinct) > 0 {
		out = q.dedupeDistinct(out)
	}
	return out, nil
}

// FindFirst executes the SELECT with LIMIT 1 and returns the first matching
// row, or ErrNotFound.
func (q *Query[T]) FindFirst(ctx context.Context) (T, error) {
	q.take = intPtr(1)
	var zero T
	rows, err := q.FindMany(ctx)
	if err != nil {
		return zero, err
	}
	if len(rows) == 0 {
		return zero, ErrNotFound
	}
	return rows[0], nil
}

// FindUnique is an alias for FindFirst intended for lookups by a unique column;
// callers supply the uniqueness via Where.
func (q *Query[T]) FindUnique(ctx context.Context) (T, error) {
	return q.FindFirst(ctx)
}

// Count executes SELECT COUNT(*) and returns the number of matching rows.
func (q *Query[T]) Count(ctx context.Context) (int64, error) {
	sqlStr, args, err := q.CountSQL()
	if err != nil {
		return 0, err
	}
	conn, err := q.executor()
	if err != nil {
		return 0, err
	}
	var n int64
	if err := conn.QueryRowContext(ctx, sqlStr, args...).Scan(&n); err != nil {
		return 0, mapError(err)
	}
	return n, nil
}

// Create inserts a single row. If the model's primary key is auto-generated and
// integer-typed, the generated id is written back into the returned value.
func (q *Query[T]) Create(ctx context.Context, value T) (T, error) {
	sqlStr, args, err := q.CreateSQL(value)
	if err != nil {
		return value, err
	}
	conn, err := q.executor()
	if err != nil {
		return value, err
	}
	res, err := conn.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return value, mapError(err)
	}
	if pk := q.model.PK; pk != nil && pk.IsAuto {
		if id, lerr := res.LastInsertId(); lerr == nil && id != 0 {
			rv := reflect.ValueOf(&value).Elem()
			fv := rv.FieldByIndex(pk.Index)
			if fv.CanSet() && isIntKind(fv.Kind()) {
				fv.SetInt(id)
			}
		}
	}
	return value, nil
}

// CreateMany inserts multiple rows in one statement and returns the number of
// rows affected.
func (q *Query[T]) CreateMany(ctx context.Context, values []T) (int64, error) {
	sqlStr, args, err := q.CreateManySQL(values)
	if err != nil {
		return 0, err
	}
	return q.exec(ctx, sqlStr, args)
}

// Update applies the column assignments to every matching row and returns the
// number of rows affected.
func (q *Query[T]) Update(ctx context.Context, data map[string]any) (int64, error) {
	sqlStr, args, err := q.UpdateSQL(data)
	if err != nil {
		return 0, err
	}
	return q.exec(ctx, sqlStr, args)
}

// Delete removes every matching row and returns the number of rows affected.
func (q *Query[T]) Delete(ctx context.Context) (int64, error) {
	sqlStr, args, err := q.DeleteSQL()
	if err != nil {
		return 0, err
	}
	return q.exec(ctx, sqlStr, args)
}

func (q *Query[T]) exec(ctx context.Context, sqlStr string, args []any) (int64, error) {
	conn, err := q.executor()
	if err != nil {
		return 0, err
	}
	res, err := conn.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, mapError(err)
	}
	return res.RowsAffected()
}

// UpdateMany is an alias for [Query.Update] using Prisma's method name: it
// applies data to every matching row and returns the number affected.
func (q *Query[T]) UpdateMany(ctx context.Context, data map[string]any) (int64, error) {
	return q.Update(ctx, data)
}

// DeleteMany is an alias for [Query.Delete] using Prisma's method name: it
// removes every matching row and returns the number affected.
func (q *Query[T]) DeleteMany(ctx context.Context) (int64, error) {
	return q.Delete(ctx)
}

// scanDest builds the slice of scan destinations for one record following the
// scan plan, allocating included relation structs as needed.
func scanDest(rec reflect.Value, plan []scanTarget) []any {
	relVals := map[*Field]reflect.Value{}
	dest := make([]any, len(plan))
	for i, t := range plan {
		if t.rel == nil {
			dest[i] = rec.FieldByIndex(t.field.Index).Addr().Interface()
			continue
		}
		rv, ok := relVals[t.rel]
		if !ok {
			rv = allocRelation(rec, t.rel)
			relVals[t.rel] = rv
		}
		dest[i] = rv.FieldByIndex(t.field.Index).Addr().Interface()
	}
	return dest
}

// allocRelation ensures the relation field on rec holds an addressable struct
// value and returns it. Pointer relation fields are allocated; struct fields
// are used in place.
func allocRelation(rec reflect.Value, rel *Field) reflect.Value {
	fv := rec.FieldByIndex(rel.Index)
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		return fv.Elem()
	}
	return fv
}

func intPtr(n int) *int { return &n }
