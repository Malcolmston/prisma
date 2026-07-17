package prisma

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
)

// memDB is a minimal in-memory database/sql driver used only for tests. It does
// not parse SQL; instead a test preloads the column set, rows and exec results
// that the next statement should return, and records the statements executed.
// This is enough to exercise the full database/sql plumbing and struct scanning
// end to end.
type memDB struct {
	mu           sync.Mutex
	cols         []string
	rows         [][]driver.Value
	countVal     int64
	lastInsertID int64
	rowsAffected int64
	queries      []recorded
}

type recorded struct {
	query string
	args  []driver.Value
}

var mem = &memDB{}

func (m *memDB) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cols = nil
	m.rows = nil
	m.countVal = 0
	m.lastInsertID = 0
	m.rowsAffected = 0
	m.queries = nil
}

func (m *memDB) record(q string, args []driver.Value) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queries = append(m.queries, recorded{query: q, args: args})
}

func (m *memDB) lastQuery() recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queries) == 0 {
		return recorded{}
	}
	return m.queries[len(m.queries)-1]
}

type memDriver struct{}

func (memDriver) Open(string) (driver.Conn, error) { return &memConn{}, nil }

type memConn struct{}

func (memConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (memConn) Close() error                        { return nil }
func (memConn) Begin() (driver.Tx, error)           { return memTx{}, nil }

type memTx struct{}

func (memTx) Commit() error   { return nil }
func (memTx) Rollback() error { return nil }

func namedToValues(args []driver.NamedValue) []driver.Value {
	vs := make([]driver.Value, len(args))
	for i, a := range args {
		vs[i] = a.Value
	}
	return vs
}

func (memConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	vs := namedToValues(args)
	mem.record(query, vs)
	mem.mu.Lock()
	defer mem.mu.Unlock()
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT COUNT") {
		return &memRows{cols: []string{"count"}, rows: [][]driver.Value{{mem.countVal}}}, nil
	}
	return &memRows{cols: append([]string(nil), mem.cols...), rows: cloneRows(mem.rows)}, nil
}

func (memConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	vs := namedToValues(args)
	mem.record(query, vs)
	mem.mu.Lock()
	defer mem.mu.Unlock()
	return memResult{lastID: mem.lastInsertID, affected: mem.rowsAffected}, nil
}

func cloneRows(in [][]driver.Value) [][]driver.Value {
	out := make([][]driver.Value, len(in))
	for i, r := range in {
		out[i] = append([]driver.Value(nil), r...)
	}
	return out
}

type memResult struct {
	lastID   int64
	affected int64
}

func (r memResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r memResult) RowsAffected() (int64, error) { return r.affected, nil }

type memRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func (r *memRows) Columns() []string { return r.cols }
func (r *memRows) Close() error      { return nil }

func (r *memRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

func init() {
	sql.Register("mem", memDriver{})
}

// openMem returns a *sql.DB backed by the shared in-memory driver.
func openMem(tb interface{ Fatal(...any) }) *sql.DB {
	db, err := sql.Open("mem", "test")
	if err != nil {
		tb.Fatal(err)
	}
	return db
}
