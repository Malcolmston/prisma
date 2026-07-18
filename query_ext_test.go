package prisma

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
)

func TestExistsSQL(t *testing.T) {
	c := newTestClient(t)
	sqlStr, args, err := NewQuery[User](c).Where(Equals("email", "a@b.com")).ExistsSQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT EXISTS(SELECT 1 FROM users WHERE email = ?)"
	if sqlStr != want {
		t.Errorf("sql = %q, want %q", sqlStr, want)
	}
	if !reflect.DeepEqual(args, []any{"a@b.com"}) {
		t.Errorf("args = %v", args)
	}
}

func TestExistsExec(t *testing.T) {
	mem.reset()
	mem.cols = []string{"exists"}
	mem.rows = [][]driver.Value{{true}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	ok, err := NewQuery[User](c).Where(Gt("id", 0)).Exists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("Exists = false, want true")
	}
}

func TestCountDistinctSQL(t *testing.T) {
	c := newTestClient(t)
	sqlStr, args, err := NewQuery[User](c).Where(Gt("id", 0)).CountDistinctSQL("email")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT COUNT(DISTINCT email) FROM users WHERE id > ?"
	if sqlStr != want {
		t.Errorf("sql = %q, want %q", sqlStr, want)
	}
	if !reflect.DeepEqual(args, []any{0}) {
		t.Errorf("args = %v", args)
	}
}

func TestCountDistinctUnknownColumn(t *testing.T) {
	c := newTestClient(t)
	if _, _, err := NewQuery[User](c).CountDistinctSQL("nope"); err == nil {
		t.Fatal("expected error for unknown column")
	}
}

func TestCountDistinctExec(t *testing.T) {
	mem.reset()
	mem.cols = []string{"c"}
	mem.rows = [][]driver.Value{{int64(3)}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	n, err := NewQuery[User](c).CountDistinct(context.Background(), "email")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("n = %d, want 3", n)
	}
}

func TestPaginate(t *testing.T) {
	c := newTestClient(t)
	cases := []struct {
		page, size int
		sql        string
		args       []any
	}{
		{1, 20, "SELECT id, name, email FROM users LIMIT ? OFFSET ?", []any{20, 0}},
		{3, 10, "SELECT id, name, email FROM users LIMIT ? OFFSET ?", []any{10, 20}},
		{0, 5, "SELECT id, name, email FROM users LIMIT ? OFFSET ?", []any{5, 0}}, // page clamped to 1
	}
	for _, tc := range cases {
		sqlStr, args, err := NewQuery[User](c).Paginate(tc.page, tc.size).FindManySQL()
		if err != nil {
			t.Fatal(err)
		}
		if sqlStr != tc.sql {
			t.Errorf("page %d: sql = %q", tc.page, sqlStr)
		}
		if !reflect.DeepEqual(args, tc.args) {
			t.Errorf("page %d: args = %v, want %v", tc.page, args, tc.args)
		}
	}
}

func TestCloneIndependent(t *testing.T) {
	c := newTestClient(t)
	base := NewQuery[User](c).Where(Gt("id", 0))
	derived := base.Clone().Where(Equals("name", "Ada"))

	baseSQL, baseArgs, err := base.FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	if baseSQL != "SELECT id, name, email FROM users WHERE id > ?" {
		t.Errorf("base mutated: %q", baseSQL)
	}
	if len(baseArgs) != 1 {
		t.Errorf("base args = %v", baseArgs)
	}

	derSQL, _, err := derived.FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	if derSQL != "SELECT id, name, email FROM users WHERE id > ? AND name = ?" {
		t.Errorf("derived = %q", derSQL)
	}
}

func TestFindFirstOrThrow(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	mem.rows = nil // no rows
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	_, err := NewQuery[User](c).Where(Equals("id", 999)).FindFirstOrThrow(context.Background())
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("err = %v, want ErrRecordNotFound (P2025)", err)
	}
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != CodeRecordNotFound {
		t.Fatalf("expected typed P2025 error, got %v", err)
	}
}

func TestFindUniqueOrThrowFound(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	mem.rows = [][]driver.Value{{int64(1), "Ada", "ada@x.com"}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	u, err := NewQuery[User](c).Where(Equals("id", 1)).FindUniqueOrThrow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Ada" {
		t.Errorf("u = %+v", u)
	}
}

func TestWhereRawAndOrWhere(t *testing.T) {
	c := newTestClient(t)
	sqlStr, args, err := NewQuery[User](c).
		Where(Equals("email", "a@b.com")).
		WhereRaw("length(name) > ?", 3).
		OrWhere(Equals("id", 1), Equals("id", 2)).
		FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE email = ? AND length(name) > ? AND (id = ? OR id = ?)"
	if sqlStr != want {
		t.Errorf("sql = %q, want %q", sqlStr, want)
	}
	if !reflect.DeepEqual(args, []any{"a@b.com", 3, 1, 2}) {
		t.Errorf("args = %v", args)
	}
}
