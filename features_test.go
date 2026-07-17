package prisma

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// Order is a numeric model used to exercise aggregations and grouping.
type Order struct {
	ID         int64   `prisma:"col=id,pk,auto"`
	CustomerID int64   `prisma:"col=customer_id"`
	Amount     float64 `prisma:"col=amount"`
	Status     string  `prisma:"col=status"`
	Meta       string  `prisma:"col=meta"`
}

func ordersClient(t *testing.T) *Client {
	t.Helper()
	c := newTestClient(t)
	if _, err := c.Register(Order{}); err != nil {
		t.Fatalf("register Order: %v", err)
	}
	return c
}

// --- Relation filters ------------------------------------------------------

func TestRelationFilterSQL(t *testing.T) {
	c := newTestClient(t)
	tests := []struct {
		name string
		cond Condition
		want string
		args []any
	}{
		{
			"some",
			Some("Posts", Equals("title", "hi")),
			"SELECT id, name, email FROM users WHERE EXISTS (SELECT 1 FROM posts WHERE posts.author_id = users.id AND title = ?)",
			[]any{"hi"},
		},
		{
			"some-bare",
			Some("Posts"),
			"SELECT id, name, email FROM users WHERE EXISTS (SELECT 1 FROM posts WHERE posts.author_id = users.id)",
			nil,
		},
		{
			"every",
			Every("Posts", Gt("id", 5)),
			"SELECT id, name, email FROM users WHERE NOT EXISTS (SELECT 1 FROM posts WHERE posts.author_id = users.id AND NOT (id > ?))",
			[]any{5},
		},
		{
			"none",
			None("Posts", Equals("title", "x")),
			"SELECT id, name, email FROM users WHERE NOT EXISTS (SELECT 1 FROM posts WHERE posts.author_id = users.id AND title = ?)",
			[]any{"x"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := NewQuery[User](c).Where(tc.cond).FindManySQL()
			if err != nil {
				t.Fatal(err)
			}
			if sql != tc.want {
				t.Errorf("sql = %q\nwant %q", sql, tc.want)
			}
			if !reflect.DeepEqual(args, tc.args) {
				t.Errorf("args = %v, want %v", args, tc.args)
			}
		})
	}
}

func TestRelationFilterToOne(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[Post](c).Where(Is("Author", Equals("name", "Bob"))).FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, title, author_id FROM posts WHERE EXISTS (SELECT 1 FROM users WHERE posts.author_id = users.id AND name = ?)"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"Bob"}) {
		t.Errorf("args = %v", args)
	}
}

func TestRelationFilterNested(t *testing.T) {
	c := newTestClient(t)
	// Users who have some post whose author is named Bob (nested relation filter).
	sql, args, err := NewQuery[User](c).
		Where(Some("Posts", Is("Author", Equals("name", "Bob")))).
		FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE EXISTS (SELECT 1 FROM posts WHERE posts.author_id = users.id AND EXISTS (SELECT 1 FROM users WHERE posts.author_id = users.id AND name = ?))"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"Bob"}) {
		t.Errorf("args = %v", args)
	}
}

func TestRelationFilterInGroup(t *testing.T) {
	c := newTestClient(t)
	sql, _, err := NewQuery[User](c).
		Where(Or(Some("Posts"), Equals("name", "x"))).
		FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE (EXISTS (SELECT 1 FROM posts WHERE posts.author_id = users.id) OR name = ?)"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
}

func TestRelationFilterErrors(t *testing.T) {
	c := newTestClient(t)
	if _, _, err := NewQuery[User](c).Where(Some("Nope")).FindManySQL(); err == nil {
		t.Error("expected error for unknown relation")
	}
}

// --- JSON path filters -----------------------------------------------------

func TestJSONPathSQL(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[User](c).
		Where(JSONEquals("email", []string{"a", "b"}, "x")).
		FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE json_extract(email, '$.a.b') = ?"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"x"}) {
		t.Errorf("args = %v", args)
	}
}

func TestJSONPathPostgres(t *testing.T) {
	c := NewClient(nil, WithDialect(Dollar))
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	sql, _, err := NewQuery[User](c).
		Where(JSONPath("email", []string{"address", "city"}, "<>", "Paris")).
		FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE (email #>> '{address,city}') <> $1"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
}

// --- Distinct / cursor / nulls ordering ------------------------------------

func TestDistinctSQL(t *testing.T) {
	c := newTestClient(t)
	sql, _, err := NewQuery[User](c).Distinct().FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT DISTINCT id, name, email FROM users" {
		t.Errorf("sql = %q", sql)
	}

	// Columns without DistinctOn fall back to plain DISTINCT.
	sql, _, _ = NewQuery[User](c).Distinct("email").FindManySQL()
	if sql != "SELECT DISTINCT id, name, email FROM users" {
		t.Errorf("fallback sql = %q", sql)
	}

	cp := NewClient(nil, WithDialect(Dollar))
	if _, err := cp.Register(User{}); err != nil {
		t.Fatal(err)
	}
	sql, _, _ = NewQuery[User](cp).Distinct("email").FindManySQL()
	if sql != "SELECT DISTINCT ON (email) id, name, email FROM users" {
		t.Errorf("distinct-on sql = %q", sql)
	}
}

func TestCursorSQL(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[User](c).
		OrderBy("id", Asc).Cursor("id", 100).Take(10).FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE id > ? ORDER BY id ASC LIMIT ?"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{100, 10}) {
		t.Errorf("args = %v", args)
	}

	// Descending cursor flips the comparison.
	sql, _, _ = NewQuery[User](c).OrderBy("id", Desc).Cursor("id", 5).FindManySQL()
	if sql != "SELECT id, name, email FROM users WHERE id < ? ORDER BY id DESC" {
		t.Errorf("desc cursor sql = %q", sql)
	}
}

func TestNullsOrderingSQL(t *testing.T) {
	c := newTestClient(t)
	sql, _, err := NewQuery[User](c).OrderByNulls("name", Asc, NullsLast).FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT id, name, email FROM users ORDER BY (name IS NULL) ASC, name ASC" {
		t.Errorf("emulated nulls-last sql = %q", sql)
	}
	sql, _, _ = NewQuery[User](c).OrderByNulls("name", Asc, NullsFirst).FindManySQL()
	if sql != "SELECT id, name, email FROM users ORDER BY (name IS NULL) DESC, name ASC" {
		t.Errorf("emulated nulls-first sql = %q", sql)
	}

	cp := NewClient(nil, WithDialect(Dollar))
	if _, err := cp.Register(User{}); err != nil {
		t.Fatal(err)
	}
	sql, _, _ = NewQuery[User](cp).OrderByNulls("name", Desc, NullsLast).FindManySQL()
	if sql != "SELECT id, name, email FROM users ORDER BY name DESC NULLS LAST" {
		t.Errorf("native nulls sql = %q", sql)
	}
}

// --- Aggregations ----------------------------------------------------------

func TestAggregateSQL(t *testing.T) {
	c := ordersClient(t)
	sql, args, err := NewQuery[Order](c).
		Where(Equals("status", "paid")).
		Aggregate().Count().Sum("amount").Avg("amount").Min("amount").Max("amount").
		SQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT COUNT(*) AS _count, SUM(amount) AS _sum_amount, AVG(amount) AS _avg_amount, " +
		"MIN(amount) AS _min_amount, MAX(amount) AS _max_amount FROM orders WHERE status = ?"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"paid"}) {
		t.Errorf("args = %v", args)
	}

	if _, _, err := NewQuery[Order](c).Aggregate().SQL(); err == nil {
		t.Error("expected error for empty aggregate")
	}
	if _, _, err := NewQuery[Order](c).Aggregate().Sum("nope").SQL(); err == nil {
		t.Error("expected error for unknown column")
	}
}

func TestAggregateExec(t *testing.T) {
	mem.reset()
	mem.cols = []string{"_count", "_sum_amount", "_avg_amount", "_min_amount", "_max_amount"}
	mem.rows = [][]driver.Value{{int64(3), float64(600), float64(200), float64(100), float64(300)}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(Order{}); err != nil {
		t.Fatal(err)
	}
	res, err := NewQuery[Order](c).
		Aggregate().Count().Sum("amount").Avg("amount").Min("amount").Max("amount").
		Exec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 3 {
		t.Errorf("count = %d", res.Count)
	}
	if res.Sum["amount"] != 600 || res.Avg["amount"] != 200 || res.Min["amount"] != 100 || res.Max["amount"] != 300 {
		t.Errorf("aggregates = %+v", res)
	}
}

// --- GroupBy ---------------------------------------------------------------

func TestGroupBySQL(t *testing.T) {
	c := ordersClient(t)
	sql, args, err := NewQuery[Order](c).
		Where(Gt("amount", 0)).
		GroupBy("status").Count().Sum("amount").
		Having(Gt("_count", 1)).
		SQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT status, COUNT(*) AS _count, SUM(amount) AS _sum_amount FROM orders " +
		"WHERE amount > ? GROUP BY status HAVING _count > ?"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{0, 1}) {
		t.Errorf("args = %v", args)
	}

	if _, _, err := NewQuery[Order](c).GroupBy("nope").Count().SQL(); err == nil {
		t.Error("expected error for unknown group column")
	}
}

func TestGroupByExec(t *testing.T) {
	mem.reset()
	mem.cols = []string{"status", "_count"}
	mem.rows = [][]driver.Value{{"paid", int64(2)}, {"pending", int64(1)}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(Order{}); err != nil {
		t.Fatal(err)
	}
	rows, err := NewQuery[Order](c).GroupBy("status").Count().Exec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["status"] != "paid" || rows[0]["_count"].(int64) != 2 {
		t.Errorf("rows = %+v", rows)
	}
}

// --- Upsert ----------------------------------------------------------------

func TestUpsertSQL(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[User](c).
		UpsertSQL([]string{"email"}, User{Name: "Ann", Email: "a@x.com"}, map[string]any{"name": "Ann2"})
	if err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO users (name, email) VALUES (?, ?) ON CONFLICT (email) DO UPDATE SET name = ?"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"Ann", "a@x.com", "Ann2"}) {
		t.Errorf("args = %v", args)
	}
}

func TestUpsertMySQL(t *testing.T) {
	c := NewClient(nil, WithDialect(MySQLDialect))
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	sql, args, err := NewQuery[User](c).
		UpsertSQL(nil, User{Name: "Ann", Email: "a@x.com"}, map[string]any{"name": "Ann2"})
	if err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO users (name, email) VALUES (?, ?) ON DUPLICATE KEY UPDATE name = ?"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"Ann", "a@x.com", "Ann2"}) {
		t.Errorf("args = %v", args)
	}
}

func TestUpsertErrors(t *testing.T) {
	c := newTestClient(t)
	if _, _, err := NewQuery[User](c).UpsertSQL([]string{"email"}, User{Name: "x"}, map[string]any{}); err == nil {
		t.Error("expected error for empty update")
	}
	if _, _, err := NewQuery[User](c).UpsertSQL(nil, User{Name: "x"}, map[string]any{"name": "y"}); err == nil {
		t.Error("expected error for missing conflict columns on ON CONFLICT")
	}
	if _, _, err := NewQuery[User](c).UpsertSQL([]string{"email"}, User{Name: "x"}, map[string]any{"nope": 1}); err == nil {
		t.Error("expected error for unknown update column")
	}
}

func TestUpsertExec(t *testing.T) {
	mem.reset()
	mem.rowsAffected = 1
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	n, err := NewQuery[User](c).Upsert(context.Background(), []string{"email"},
		User{Name: "Ann", Email: "a@x.com"}, map[string]any{"name": "Ann2"})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

// --- CreateMany skip-duplicates --------------------------------------------

func TestCreateManySkipDuplicatesSQL(t *testing.T) {
	c := newTestClient(t)
	sql, _, err := NewQuery[User](c).SkipDuplicates().CreateManySQL([]User{
		{Name: "A", Email: "a@x.com"},
		{Name: "B", Email: "b@x.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "INSERT INTO users (name, email) VALUES (?, ?), (?, ?) ON CONFLICT DO NOTHING"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}

	cm := NewClient(nil, WithDialect(MySQLDialect))
	if _, err := cm.Register(User{}); err != nil {
		t.Fatal(err)
	}
	sql, _, _ = NewQuery[User](cm).SkipDuplicates().CreateManySQL([]User{{Name: "A", Email: "a@x.com"}})
	if sql != "INSERT IGNORE INTO users (name, email) VALUES (?, ?)" {
		t.Errorf("mysql skip sql = %q", sql)
	}
}

// --- UpdateMany / DeleteMany -----------------------------------------------

func TestUpdateDeleteMany(t *testing.T) {
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	mem.reset()
	mem.rowsAffected = 5
	n, err := NewQuery[User](c).Where(Gt("id", 1)).UpdateMany(ctx, map[string]any{"name": "X"})
	if err != nil || n != 5 {
		t.Fatalf("UpdateMany n=%d err=%v", n, err)
	}

	mem.reset()
	mem.rowsAffected = 2
	n, err = NewQuery[User](c).Where(Lt("id", 9)).DeleteMany(ctx)
	if err != nil || n != 2 {
		t.Fatalf("DeleteMany n=%d err=%v", n, err)
	}
}

// --- Transactions ----------------------------------------------------------

func TestInteractiveTransaction(t *testing.T) {
	mem.reset()
	mem.rowsAffected = 1
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	err := c.Transaction(context.Background(), func(tx *Tx) error {
		_, err := NewTxQuery[User](tx).Where(Equals("id", 1)).Update(context.Background(), map[string]any{"name": "T"})
		return err
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if got := mem.lastQuery().query; got != "UPDATE users SET name = ? WHERE id = ?" {
		t.Errorf("query = %q", got)
	}
}

func TestTransactionRollback(t *testing.T) {
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("boom")
	err := c.Transaction(context.Background(), func(tx *Tx) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want sentinel", err)
	}
}

func TestBatchTransaction(t *testing.T) {
	mem.reset()
	mem.rowsAffected = 1
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	results, err := c.Batch(context.Background(),
		func(ctx context.Context, tx *Tx) (int64, error) {
			u, err := NewTxQuery[User](tx).Create(ctx, User{Name: "A"})
			return u.ID, err
		},
		func(ctx context.Context, tx *Tx) (int64, error) {
			return NewTxQuery[User](tx).Where(Gt("id", 0)).Delete(ctx)
		},
	)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("results = %v", results)
	}
}

func TestTransactionNoDB(t *testing.T) {
	c := NewClient(nil)
	if err := c.Transaction(context.Background(), func(tx *Tx) error { return nil }); err == nil {
		t.Error("expected error with nil db")
	}
}

// --- Raw queries -----------------------------------------------------------

func TestQueryRaw(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id", "name"}
	mem.rows = [][]driver.Value{{int64(1), "Ann"}, {int64(2), "Bob"}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	rows, err := c.QueryRaw(context.Background(), "SELECT id, name FROM users WHERE id > ?", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["name"] != "Ann" || rows[1]["id"].(int64) != 2 {
		t.Errorf("rows = %+v", rows)
	}
	if got := mem.lastQuery(); !reflect.DeepEqual(got.args, []driver.Value{int64(0)}) {
		t.Errorf("args = %v", got.args)
	}
}

func TestQueryRawInto(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	mem.rows = [][]driver.Value{{int64(7), "Cara", "cara@x.com"}}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	users, err := QueryRawInto[User](context.Background(), c, "SELECT * FROM users")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != 7 || users[0].Name != "Cara" {
		t.Errorf("users = %+v", users)
	}
}

func TestExecuteRaw(t *testing.T) {
	mem.reset()
	mem.rowsAffected = 3
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	n, err := c.ExecuteRaw(context.Background(), "DELETE FROM users WHERE id < ?", 10)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestRawNoDB(t *testing.T) {
	c := NewClient(nil)
	if _, err := c.QueryRaw(context.Background(), "SELECT 1"); err == nil {
		t.Error("expected error (QueryRaw)")
	}
	if _, err := c.ExecuteRaw(context.Background(), "DELETE FROM x"); err == nil {
		t.Error("expected error (ExecuteRaw)")
	}
}

// --- Nested writes ---------------------------------------------------------

func TestNestedCreateStatements(t *testing.T) {
	c := newTestClient(t)
	stmts, err := NewQuery[User](c).
		NestedCreate(User{Name: "Ada", Email: "ada@x.com"}).
		Create("Posts", Post{Title: "Hello"}).
		Connect("Posts", Equals("id", 7)).
		Update("Posts", []Condition{Equals("id", 8)}, map[string]any{"title": "Renamed"}).
		Statements(int64(42))
	if err != nil {
		t.Fatal(err)
	}
	want := []Statement{
		{"INSERT INTO users (name, email) VALUES (?, ?)", []any{"Ada", "ada@x.com"}},
		{"INSERT INTO posts (title, author_id) VALUES (?, ?)", []any{"Hello", int64(42)}},
		{"UPDATE posts SET author_id = ? WHERE id = ?", []any{int64(42), 7}},
		{"UPDATE posts SET title = ? WHERE author_id = ? AND id = ?", []any{"Renamed", int64(42), 8}},
	}
	if len(stmts) != len(want) {
		t.Fatalf("got %d statements, want %d: %+v", len(stmts), len(want), stmts)
	}
	for i := range want {
		if stmts[i].SQL != want[i].SQL {
			t.Errorf("stmt[%d] sql = %q\nwant %q", i, stmts[i].SQL, want[i].SQL)
		}
		if !reflect.DeepEqual(stmts[i].Args, want[i].Args) {
			t.Errorf("stmt[%d] args = %v, want %v", i, stmts[i].Args, want[i].Args)
		}
	}
}

func TestNestedCreateExec(t *testing.T) {
	mem.reset()
	mem.lastInsertID = 42
	mem.rowsAffected = 1
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	for _, m := range []any{User{}, Post{}} {
		if _, err := c.Register(m); err != nil {
			t.Fatal(err)
		}
	}
	u, err := NewQuery[User](c).
		NestedCreate(User{Name: "Ada", Email: "ada@x.com"}).
		Create("Posts", Post{Title: "Hello"}).
		Exec(context.Background())
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if u.ID != 42 {
		t.Errorf("parent id = %d, want 42", u.ID)
	}
	// The child insert should carry the parent's id as author_id.
	last := mem.lastQuery()
	if last.query != "INSERT INTO posts (title, author_id) VALUES (?, ?)" {
		t.Errorf("child query = %q", last.query)
	}
	if !reflect.DeepEqual(last.args, []driver.Value{"Hello", int64(42)}) {
		t.Errorf("child args = %v", last.args)
	}
}

func TestNestedConnectOrCreate(t *testing.T) {
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	for _, m := range []any{User{}, Post{}} {
		if _, err := c.Register(m); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()

	// Existing row: connectOrCreate connects (UPDATE), does not insert a post.
	mem.reset()
	mem.lastInsertID = 10
	mem.rowsAffected = 1
	mem.countVal = 1
	if _, err := NewQuery[User](c).
		NestedCreate(User{Name: "A", Email: "a@x.com"}).
		ConnectOrCreate("Posts", []Condition{Equals("id", 3)}, Post{Title: "New"}).
		Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if q := mem.lastQuery().query; q != "UPDATE posts SET author_id = ? WHERE id = ?" {
		t.Errorf("connect branch query = %q", q)
	}

	// Missing row: connectOrCreate inserts the post.
	mem.reset()
	mem.lastInsertID = 11
	mem.rowsAffected = 1
	mem.countVal = 0
	if _, err := NewQuery[User](c).
		NestedCreate(User{Name: "B", Email: "b@x.com"}).
		ConnectOrCreate("Posts", []Condition{Equals("id", 3)}, Post{Title: "New"}).
		Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if q := mem.lastQuery().query; q != "INSERT INTO posts (title, author_id) VALUES (?, ?)" {
		t.Errorf("create branch query = %q", q)
	}
}

func TestNestedWriteErrors(t *testing.T) {
	c := newTestClient(t)
	if _, err := NewQuery[User](c).NestedCreate(User{Name: "x"}).
		Create("Nope", Post{}).Statements(int64(1)); err == nil {
		t.Error("expected error for unknown relation")
	}
	// Author is a to-one relation on Post; nested write requires to-many.
	if _, err := NewQuery[Post](c).NestedCreate(Post{Title: "x"}).
		Create("Author", User{}).Statements(int64(1)); err == nil {
		t.Error("expected error for to-one relation")
	}
	// Wrong child type.
	if _, err := NewQuery[User](c).NestedCreate(User{Name: "x"}).
		Create("Posts", User{}).Statements(int64(1)); err == nil {
		t.Error("expected error for wrong child type")
	}
}

// --- Typed errors ----------------------------------------------------------

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		raw  string
		code string
	}{
		{"UNIQUE constraint failed: users.email", CodeUniqueViolation},
		{"duplicate key value violates unique constraint", CodeUniqueViolation},
		{"Duplicate entry 'x' for key 'email'", CodeUniqueViolation},
		{"FOREIGN KEY constraint failed", CodeForeignKeyViolation},
		{"null value in column \"name\" violates not-null constraint", CodeNotNullViolation},
		{"value too long for type character varying(3)", CodeValueTooLong},
	}
	for _, tc := range cases {
		got := mapError(errors.New(tc.raw))
		var pe *Error
		if !errors.As(got, &pe) || pe.Code != tc.code {
			t.Errorf("mapError(%q) = %v, want code %s", tc.raw, got, tc.code)
		}
	}
	// errors.Is against sentinels.
	if !errors.Is(mapError(errors.New("UNIQUE constraint failed")), ErrUniqueViolation) {
		t.Error("expected ErrUniqueViolation match")
	}
	// Unrecognized errors pass through unchanged.
	plain := errors.New("some other error")
	if mapError(plain) != plain {
		t.Error("expected passthrough for unknown error")
	}
	if mapError(nil) != nil {
		t.Error("expected nil passthrough")
	}
}

func TestErrorMethods(t *testing.T) {
	cause := errors.New("root cause")
	e := &Error{Code: CodeUniqueViolation, Message: "dup", Cause: cause}
	if e.Error() != "prisma: P2002: dup: root cause" {
		t.Errorf("Error() = %q", e.Error())
	}
	if !errors.Is(e, cause) {
		t.Error("expected Unwrap to reach cause")
	}
	bare := &Error{Code: CodeRecordNotFound, Message: "missing"}
	if bare.Error() != "prisma: P2025: missing" {
		t.Errorf("bare Error() = %q", bare.Error())
	}
	if errors.Is(e, ErrForeignKeyViolation) {
		t.Error("P2002 should not match P2003 sentinel")
	}
}

func TestGroupByAllAggregates(t *testing.T) {
	c := ordersClient(t)
	sql, _, err := NewQuery[Order](c).
		GroupBy("status").Avg("amount").Min("amount").Max("amount").
		SQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT status, AVG(amount) AS _avg_amount, MIN(amount) AS _min_amount, " +
		"MAX(amount) AS _max_amount FROM orders GROUP BY status"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if _, err := NewQuery[Order](c).GroupBy("status").Min("nope").Exec(context.Background()); err == nil {
		t.Error("expected error for unknown column in group aggregate")
	}
}

func TestTxRaw(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id"}
	mem.rows = [][]driver.Value{{int64(1)}}
	mem.rowsAffected = 2
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	err := c.Transaction(context.Background(), func(tx *Tx) error {
		rows, err := tx.QueryRaw(context.Background(), "SELECT id FROM users")
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			t.Errorf("rows = %+v", rows)
		}
		n, err := tx.ExecuteRaw(context.Background(), "DELETE FROM users")
		if err != nil {
			return err
		}
		if n != 2 {
			t.Errorf("n = %d", n)
		}
		if tx.Client() != c || tx.SQLTx() == nil {
			t.Error("tx accessors")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestErrorExecMapping(t *testing.T) {
	mem.reset()
	mem.execErr = fmt.Errorf("UNIQUE constraint failed: users.email")
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	_, err := NewQuery[User](c).Create(context.Background(), User{Name: "Z"})
	if !errors.Is(err, ErrUniqueViolation) {
		t.Errorf("err = %v, want ErrUniqueViolation", err)
	}
	mem.execErr = nil
}
