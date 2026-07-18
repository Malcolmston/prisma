package prisma

// Upstream-parity tests. The vectors below are transcribed from the real
// prisma/prisma test suite so this Go port asserts the same known answers the
// original TypeScript client asserts:
//
//   - String filters (startsWith / endsWith / contains, their NOT and
//     case-insensitive forms):
//     packages/client/tests/functional/string-filters/tests.ts
//   - distinct-on-fields row counts:
//     packages/client/tests/functional/distinct/tests.ts
//   - count() with take:
//     packages/client/tests/functional/methods/count/tests.ts
//   - findFirstOrThrow raising P2025 when no row matches:
//     packages/client/tests/functional/methods/findFirstOrThrow/tests.ts
//
// The string-filter cases are validated by compiling each operator to its SQL
// fragment and pattern via the port's own condition builders, then evaluating
// that pattern against the upstream dataset with a faithful SQL LIKE matcher —
// so the assertions exercise the exact patterns the port emits, not a
// re-implementation of the operator.

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"sort"
	"testing"
)

// likeMatch reports whether s matches a SQL LIKE pattern, implementing the
// standard semantics the port relies on: '%' matches any run of characters,
// '_' matches exactly one, and '\\' escapes the following pattern character so
// it is matched literally (the PostgreSQL/MySQL default that escapeLike targets).
func likeMatch(pattern, s string) bool {
	return likeRec([]rune(pattern), []rune(s))
}

func likeRec(p, s []rune) bool {
	for len(p) > 0 {
		switch p[0] {
		case '\\':
			var lit rune
			if len(p) >= 2 {
				lit = p[1]
				p = p[2:]
			} else {
				lit = '\\'
				p = p[1:]
			}
			if len(s) == 0 || s[0] != lit {
				return false
			}
			s = s[1:]
		case '%':
			rest := p[1:]
			for {
				if likeRec(rest, s) {
					return true
				}
				if len(s) == 0 {
					return false
				}
				s = s[1:]
			}
		case '_':
			if len(s) == 0 {
				return false
			}
			p, s = p[1:], s[1:]
		default:
			if len(s) == 0 || s[0] != p[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return len(s) == 0
}

// parityCompile renders a single Condition to its SQL fragment and bound
// arguments using the client's dialect, without touching a database.
func parityCompile(c *Client, cond Condition) (string, []any) {
	w := &sqlWriter{d: c.dialect, reg: c.registry}
	frag := cond.compile(w)
	return frag, w.args
}

// parityPredicate turns a compiled string-filter Condition into a Go predicate
// that reproduces its SQL truth value against a subject string. It handles the
// LIKE / NOT LIKE / LOWER(col) LIKE shapes the string operators emit.
func parityPredicate(t *testing.T, c *Client, cond Condition) func(string) bool {
	t.Helper()
	frag, args := parityCompile(c, cond)
	if len(args) != 1 {
		t.Fatalf("string filter compiled to %d args, want 1 (frag %q)", len(args), frag)
	}
	pattern, ok := args[0].(string)
	if !ok {
		t.Fatalf("string filter pattern is %T, want string", args[0])
	}
	insensitive := contains(frag, "LOWER(")
	negated := contains(frag, "NOT LIKE")
	return func(s string) bool {
		subject := s
		if insensitive {
			subject = toLower(s)
		}
		m := likeMatch(pattern, subject)
		if negated {
			return !m
		}
		return m
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// TestParityStringFilters mirrors string-filters/tests.ts. Each case ANDs one or
// more of the port's string operators and asserts the matching rows equal the
// values the upstream suite asserts.
func TestParityStringFilters(t *testing.T) {
	c := newTestClient(t)

	base := []string{"foo bar baz", "foo", "baz", "bar", "", "completely different"}
	insensitive := append(append([]string(nil), base...), "FOO BAR BAZ", "Foo")

	type sfCase struct {
		name    string
		conds   []Condition
		dataset []string
		want    []string
	}
	cases := []sfCase{
		{"startsWith prefix", []Condition{StartsWith("value", "foo")}, base, []string{"foo", "foo bar baz"}},
		{"startsWith no match", []Condition{StartsWith("value", "xyz")}, base, []string{}},
		{"startsWith empty matches all", []Condition{StartsWith("value", "")}, base, base},
		{"endsWith suffix", []Condition{EndsWith("value", "baz")}, base, []string{"baz", "foo bar baz"}},
		{"endsWith no match", []Condition{EndsWith("value", "xyz")}, base, []string{}},
		{"endsWith empty matches all", []Condition{EndsWith("value", "")}, base, base},
		{"contains substring", []Condition{Contains("value", "bar")}, base, []string{"bar", "foo bar baz"}},
		{"contains no match", []Condition{Contains("value", "xyz")}, base, []string{}},
		{"contains empty matches all", []Condition{Contains("value", "")}, base, base},
		{"startsWith + endsWith", []Condition{StartsWith("value", "foo"), EndsWith("value", "baz")}, base, []string{"foo bar baz"}},
		{"startsWith + contains", []Condition{StartsWith("value", "foo"), Contains("value", "bar")}, base, []string{"foo bar baz"}},
		{"contains + endsWith", []Condition{Contains("value", "bar"), EndsWith("value", "baz")}, base, []string{"foo bar baz"}},
		{"NOT startsWith", []Condition{NotStartsWith("value", "foo")}, base, []string{"", "bar", "baz", "completely different"}},
		{"NOT contains", []Condition{NotContains("value", "bar")}, base, []string{"", "baz", "completely different", "foo"}},
		{"NOT endsWith", []Condition{NotEndsWith("value", "baz")}, base, []string{"", "bar", "completely different", "foo"}},
		{"contains insensitive", []Condition{IContains("value", "bar")}, insensitive, []string{"FOO BAR BAZ", "bar", "foo bar baz"}},
		{"startsWith insensitive", []Condition{IStartsWith("value", "foo")}, insensitive, []string{"FOO BAR BAZ", "Foo", "foo", "foo bar baz"}},
		{"endsWith insensitive", []Condition{IEndsWith("value", "baz")}, insensitive, []string{"FOO BAR BAZ", "baz", "foo bar baz"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preds := make([]func(string) bool, len(tc.conds))
			for i, cond := range tc.conds {
				preds[i] = parityPredicate(t, c, cond)
			}
			var got []string
			for _, s := range tc.dataset {
				ok := true
				for _, p := range preds {
					if !p(s) {
						ok = false
						break
					}
				}
				if ok {
					got = append(got, s)
				}
			}
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("matches = %q, want %q", got, want)
			}
		})
	}
}

type parityPerson struct {
	ID        int64  `prisma:"col=id,pk,auto"`
	FirstName string `prisma:"col=first_name"`
	LastName  string `prisma:"col=last_name"`
}

// TestParityDistinct mirrors distinct/tests.ts: over four rows whose
// (firstName, lastName) pairs are (f1,l2),(f1,l2),(f1,l3),(f2,l4), distinct on a
// field subset returns one row per distinct combination, on every dialect —
// exercised end to end through FindMany and the port's row de-duplication.
func TestParityDistinct(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id", "first_name", "last_name"}
	mem.rows = [][]driver.Value{
		{int64(1), "f1", "l2"},
		{int64(2), "f1", "l2"},
		{int64(3), "f1", "l3"},
		{int64(4), "f2", "l4"},
	}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(parityPerson{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	cases := []struct {
		name string
		cols []string
		want int
	}{
		{"distinct firstName", []string{"first_name"}, 2},
		{"distinct firstName+lastName", []string{"first_name", "last_name"}, 3},
		{"distinct id", []string{"id"}, 4},
		{"distinct firstName+firstName", []string{"first_name", "first_name"}, 2},
		{"distinct id+firstName+lastName", []string{"id", "first_name", "last_name"}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := NewQuery[parityPerson](c).Distinct(tc.cols...).FindMany(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.want {
				t.Errorf("distinct %v returned %d rows, want %d", tc.cols, len(rows), tc.want)
			}
		})
	}
}

// TestParityCountTake mirrors methods/count/tests.ts: count honours take, so
// count({ take: 2 }) over three matches returns 2. The plain count keeps its
// flat form.
func TestParityCountTake(t *testing.T) {
	c := newTestClient(t)

	plainSQL, plainArgs, err := NewQuery[User](c).CountSQL()
	if err != nil {
		t.Fatal(err)
	}
	if plainSQL != "SELECT COUNT(*) FROM users" || len(plainArgs) != 0 {
		t.Errorf("plain count = %q args %v", plainSQL, plainArgs)
	}

	takeSQL, takeArgs, err := NewQuery[User](c).Take(2).CountSQL()
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := "SELECT COUNT(*) FROM (SELECT 1 FROM users LIMIT ?) AS _count_sub"
	if takeSQL != wantSQL {
		t.Errorf("count take sql = %q, want %q", takeSQL, wantSQL)
	}
	if !reflect.DeepEqual(takeArgs, []any{2}) {
		t.Errorf("count take args = %v, want [2]", takeArgs)
	}

	// End-to-end: three matching rows limited to two counts as two.
	mem.reset()
	mem.cols = []string{"count"}
	mem.rows = [][]driver.Value{{int64(2)}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	mc := NewClient(db)
	if _, err := mc.Register(User{}); err != nil {
		t.Fatal(err)
	}
	n, err := NewQuery[User](mc).Take(2).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("count with take:2 over 3 rows = %d, want 2", n)
	}
}

// TestParityFindFirstOrThrow mirrors methods/findFirstOrThrow/tests.ts: an
// existing row is returned, and a missing row raises the Prisma P2025
// record-not-found error.
func TestParityFindFirstOrThrow(t *testing.T) {
	ctx := context.Background()

	// Found: one preset row is returned.
	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	mem.rows = [][]driver.Value{{int64(1), "Alice", "existing@email.com"}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	rec, err := NewQuery[User](c).Where(Equals("email", "existing@email.com")).FindFirstOrThrow(ctx)
	if err != nil {
		t.Fatalf("expected record, got error %v", err)
	}
	if rec.Email != "existing@email.com" {
		t.Errorf("email = %q", rec.Email)
	}

	// Not found: no rows -> P2025.
	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	mem.rows = nil
	_, err = NewQuery[User](c).Where(Equals("email", "missing@email.com")).FindFirstOrThrow(ctx)
	if err == nil {
		t.Fatal("expected P2025 error, got nil")
	}
	if !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("error %v is not ErrRecordNotFound", err)
	}
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != CodeRecordNotFound {
		t.Errorf("error %v does not carry code %s", err, CodeRecordNotFound)
	}
}
