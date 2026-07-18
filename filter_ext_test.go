package prisma

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"
)

// compileCond compiles a single condition against a fresh writer using the
// User model as the base and returns the SQL fragment plus bound args.
func compileCondExt(t *testing.T, c Condition, d Dialect) (string, []any) {
	t.Helper()
	cl := NewClient(nil, WithDialect(d))
	m, err := cl.Register(User{})
	if err != nil {
		t.Fatal(err)
	}
	w := &sqlWriter{d: d, reg: cl.registry, base: m}
	s := c.compile(w)
	if w.err != nil {
		t.Fatalf("compile error: %v", w.err)
	}
	return s, w.args
}

func TestFilterExtSQL(t *testing.T) {
	cases := []struct {
		name string
		cond Condition
		sql  string
		args []any
	}{
		{"IsNull", IsNull("name"), "name IS NULL", nil},
		{"IsNotNull", IsNotNull("name"), "name IS NOT NULL", nil},
		{"Between", Between("id", 1, 10), "id BETWEEN ? AND ?", []any{1, 10}},
		{"NotBetween", NotBetween("id", 1, 10), "id NOT BETWEEN ? AND ?", []any{1, 10}},
		{"NotContains", NotContains("name", "x"), "name NOT LIKE ?", []any{"%x%"}},
		{"NotStartsWith", NotStartsWith("name", "x"), "name NOT LIKE ?", []any{"x%"}},
		{"NotEndsWith", NotEndsWith("name", "x"), "name NOT LIKE ?", []any{"%x"}},
		{"IContains", IContains("name", "Ada"), "LOWER(name) LIKE ?", []any{"%ada%"}},
		{"IStartsWith", IStartsWith("name", "Ada"), "LOWER(name) LIKE ?", []any{"ada%"}},
		{"IEndsWith", IEndsWith("name", "Ada"), "LOWER(name) LIKE ?", []any{"%ada"}},
		{"IEquals", IEquals("name", "Ada"), "LOWER(name) = ?", []any{"ada"}},
		{"RawCondition", RawCondition("length(name) > ?", 3), "length(name) > ?", []any{3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, args := compileCondExt(t, tc.cond, Question)
			if s != tc.sql {
				t.Errorf("sql = %q, want %q", s, tc.sql)
			}
			if !reflect.DeepEqual(args, tc.args) {
				t.Errorf("args = %v, want %v", args, tc.args)
			}
		})
	}
}

func TestFilterExtEscapesWildcards(t *testing.T) {
	s, args := compileCondExt(t, IContains("name", "50%_x"), Question)
	if s != "LOWER(name) LIKE ?" {
		t.Fatalf("sql = %q", s)
	}
	if args[0] != `%50\%\_x%` {
		t.Errorf("escaped pattern = %q", args[0])
	}
}

func TestRawConditionDollarPlaceholders(t *testing.T) {
	s, args := compileCondExt(t, RawCondition("a = ? OR b = ?", 1, 2), Dollar)
	if s != "a = $1 OR b = $2" {
		t.Errorf("sql = %q", s)
	}
	if !reflect.DeepEqual(args, []any{1, 2}) {
		t.Errorf("args = %v", args)
	}
}

func TestRawConditionArityMismatch(t *testing.T) {
	cl := NewClient(nil)
	m, _ := cl.Register(User{})
	w := &sqlWriter{d: Question, reg: cl.registry, base: m}
	_ = RawCondition("a = ? AND b = ?", 1).compile(w)
	if w.err == nil {
		t.Fatal("expected arity error")
	}
}

func TestFilterExtInQuery(t *testing.T) {
	c := newTestClient(t)
	sqlStr, args, err := NewQuery[User](c).
		Where(Between("id", 1, 5), IsNotNull("email")).
		FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE id BETWEEN ? AND ? AND email IS NOT NULL"
	if sqlStr != want {
		t.Errorf("sql = %q, want %q", sqlStr, want)
	}
	if !reflect.DeepEqual(args, []any{1, 5}) {
		t.Errorf("args = %v", args)
	}
}

func TestIContainsExecRoundTrip(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	mem.rows = [][]driver.Value{{int64(1), "Ada", "ada@x.com"}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	got, err := NewQuery[User](c).Where(IContains("name", "AD")).FindMany(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Ada" {
		t.Fatalf("rows = %+v", got)
	}
	if q := mem.lastQuery().query; q != "SELECT id, name, email FROM users WHERE LOWER(name) LIKE ?" {
		t.Errorf("query = %q", q)
	}
}
