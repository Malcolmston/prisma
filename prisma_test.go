package prisma

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
)

// --- Test models -----------------------------------------------------------

type User struct {
	ID    int64  `prisma:"col=id,pk,auto"`
	Name  string `prisma:"col=name"`
	Email string `prisma:"col=email"`
	Posts []Post `prisma:"relation,fk=author_id,references=id"`
}

type Post struct {
	ID       int64  `prisma:"col=id,pk,auto"`
	Title    string `prisma:"col=title"`
	AuthorID int64  `prisma:"col=author_id"`
	Author   *User  `prisma:"relation,fk=author_id,references=id"`
}

// Widget uses no tags at all, exercising the naming defaults.
type Widget struct {
	ID   int64
	Name string
}

// Account overrides the table name via TableNamer.
type Account struct {
	ID      int64 `prisma:"pk"`
	Balance int64 `prisma:"col=balance"`
}

func (Account) TableName() string { return "accounts_v2" }

func newTestClient(t *testing.T) *Client {
	t.Helper()
	c := NewClient(nil)
	for _, m := range []any{User{}, Post{}, Widget{}, Account{}} {
		if _, err := c.Register(m); err != nil {
			t.Fatalf("register %T: %v", m, err)
		}
	}
	return c
}

// --- Naming helpers --------------------------------------------------------

func TestSnakeCase(t *testing.T) {
	cases := map[string]string{
		"ID":         "id",
		"UserID":     "user_id",
		"HTTPServer": "http_server",
		"Name":       "name",
		"AuthorID":   "author_id",
		"OAuthToken": "o_auth_token",
	}
	for in, want := range cases {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPluralize(t *testing.T) {
	cases := map[string]string{"user": "users", "post": "posts", "class": "class", "category": "categories", "": ""}
	for in, want := range cases {
		if got := pluralize(in); got != want {
			t.Errorf("pluralize(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- Registration ----------------------------------------------------------

func TestRegisterModel(t *testing.T) {
	r := NewRegistry()
	m, err := r.Register(User{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Table != "users" {
		t.Errorf("table = %q, want users", m.Table)
	}
	if got := m.Columns(); !reflect.DeepEqual(got, []string{"id", "name", "email"}) {
		t.Errorf("columns = %v", got)
	}
	if m.PK == nil || m.PK.Column != "id" || !m.PK.IsAuto {
		t.Errorf("pk = %+v", m.PK)
	}
	if len(m.Relations) != 1 || m.Relations[0].GoName != "Posts" {
		t.Errorf("relations = %+v", m.Relations)
	}
	rel, ok := m.RelationByName("Posts")
	if !ok || rel.Relation.ForeignKey != "author_id" || !rel.Relation.Many {
		t.Errorf("Posts relation = %+v", rel)
	}
	if _, ok := m.FieldByColumn("email"); !ok {
		t.Error("expected email column")
	}
	// Re-register returns the cached model.
	m2, _ := r.Register(&User{})
	if m2 != m {
		t.Error("expected cached model on re-register")
	}
}

func TestRegisterDefaults(t *testing.T) {
	m, err := NewRegistry().Register(Widget{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Table != "widgets" {
		t.Errorf("table = %q", m.Table)
	}
	if m.PK == nil || m.PK.Column != "id" || !m.PK.IsAuto {
		t.Errorf("expected id pk fallback, got %+v", m.PK)
	}
}

func TestRegisterTableNamer(t *testing.T) {
	m, err := NewRegistry().Register(Account{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Table != "accounts_v2" {
		t.Errorf("table = %q, want accounts_v2", m.Table)
	}
}

func TestRegisterErrors(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Register(nil); err == nil {
		t.Error("expected error for nil")
	}
	if _, err := r.Register(42); err == nil {
		t.Error("expected error for non-struct")
	}
	type Empty struct{ unexported int } //nolint:unused
	if _, err := r.Register(Empty{}); err == nil {
		t.Error("expected error for model with no columns")
	}
	type BadRel struct {
		ID  int64 `prisma:"pk"`
		Rel *User `prisma:"relation"`
	}
	if _, err := r.Register(BadRel{}); err == nil {
		t.Error("expected error for relation without fk")
	}
}

// --- Conditions ------------------------------------------------------------

func compileCond(t *testing.T, c Condition) (string, []any) {
	t.Helper()
	w := &sqlWriter{d: Question}
	return c.compile(w), w.args
}

func TestOperators(t *testing.T) {
	tests := []struct {
		name string
		cond Condition
		sql  string
		args []any
	}{
		{"equals", Equals("id", 1), "id = ?", []any{1}},
		{"not", Not("id", 1), "id <> ?", []any{1}},
		{"lt", Lt("age", 18), "age < ?", []any{18}},
		{"lte", Lte("age", 18), "age <= ?", []any{18}},
		{"gt", Gt("age", 18), "age > ?", []any{18}},
		{"gte", Gte("age", 18), "age >= ?", []any{18}},
		{"in", In("id", 1, 2, 3), "id IN (?, ?, ?)", []any{1, 2, 3}},
		{"in-empty", In("id"), "1=0", nil},
		{"notin", NotIn("id", 1), "id NOT IN (?)", []any{1}},
		{"notin-empty", NotIn("id"), "1=1", nil},
		{"contains", Contains("name", "ann"), "name LIKE ?", []any{"%ann%"}},
		{"starts", StartsWith("name", "an"), "name LIKE ?", []any{"an%"}},
		{"ends", EndsWith("name", "ne"), "name LIKE ?", []any{"%ne"}},
		{"contains-escape", Contains("name", "50%_x"), "name LIKE ?", []any{`%50\%\_x%`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSQL, gotArgs := compileCond(t, tc.cond)
			if gotSQL != tc.sql {
				t.Errorf("sql = %q, want %q", gotSQL, tc.sql)
			}
			if !reflect.DeepEqual(gotArgs, tc.args) {
				t.Errorf("args = %v, want %v", gotArgs, tc.args)
			}
		})
	}
}

func TestGrouping(t *testing.T) {
	c := Or(
		And(Equals("a", 1), Gt("b", 2)),
		NotGroup(Equals("c", 3)),
	)
	sql, args := compileCond(t, c)
	want := "((a = ? AND b > ?) OR NOT (c = ?))"
	if sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{1, 2, 3}) {
		t.Errorf("args = %v", args)
	}
	// Single-element group collapses without parentheses.
	if s, _ := compileCond(t, And(Equals("a", 1))); s != "a = ?" {
		t.Errorf("single group = %q", s)
	}
	// Empty group compiles to empty string.
	if s, _ := compileCond(t, And()); s != "" {
		t.Errorf("empty group = %q", s)
	}
	if s, _ := compileCond(t, NotGroup(And())); s != "" {
		t.Errorf("not(empty) = %q", s)
	}
}

// --- SQL generation --------------------------------------------------------

func TestFindManySQL(t *testing.T) {
	c := newTestClient(t)
	q := NewQuery[User](c).
		Where(Equals("email", "a@b.com"), Gt("id", 10)).
		OrderBy("name", Asc).
		OrderBy("id", Desc).
		Take(20).
		Skip(5)
	sql, args, err := q.FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE email = ? AND id > ? ORDER BY name ASC, id DESC LIMIT ? OFFSET ?"
	if sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"a@b.com", 10, 20, 5}) {
		t.Errorf("args = %v", args)
	}
}

func TestSelectProjection(t *testing.T) {
	c := newTestClient(t)
	sql, _, err := NewQuery[User](c).Select("id", "name").FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT id, name FROM users" {
		t.Errorf("sql = %q", sql)
	}
	if _, _, err := NewQuery[User](c).Select("nope").FindManySQL(); err == nil {
		t.Error("expected error for unknown column")
	}
}

func TestIncludeSQL(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[Post](c).
		Include("Author").
		Where(Equals("id", 7)).
		FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT posts.id, posts.title, posts.author_id, users.id, users.name, users.email " +
		"FROM posts LEFT JOIN users ON posts.author_id = users.id WHERE id = ?"
	if sql != want {
		t.Errorf("sql = %q\nwant %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{7}) {
		t.Errorf("args = %v", args)
	}
	if _, _, err := NewQuery[Post](c).Include("Nope").FindManySQL(); err == nil {
		t.Error("expected error for unknown relation")
	}
}

func TestDollarDialect(t *testing.T) {
	c := NewClient(nil, WithDialect(Dollar))
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	sql, _, err := NewQuery[User](c).Where(Equals("id", 1), Equals("name", "x")).FindManySQL()
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT id, name, email FROM users WHERE id = $1 AND name = $2"
	if sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
}

func TestCountDeleteSQL(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[User](c).Where(Gt("id", 3)).CountSQL()
	if err != nil {
		t.Fatal(err)
	}
	if sql != "SELECT COUNT(*) FROM users WHERE id > ?" || !reflect.DeepEqual(args, []any{3}) {
		t.Errorf("count sql = %q args %v", sql, args)
	}
	sql, args, err = NewQuery[User](c).Where(Equals("id", 9)).DeleteSQL()
	if err != nil {
		t.Fatal(err)
	}
	if sql != "DELETE FROM users WHERE id = ?" || !reflect.DeepEqual(args, []any{9}) {
		t.Errorf("delete sql = %q args %v", sql, args)
	}
}

func TestUpdateSQL(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[User](c).
		Where(Equals("id", 1)).
		UpdateSQL(map[string]any{"name": "Ann", "email": "ann@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Columns are sorted: email, name.
	want := "UPDATE users SET email = ?, name = ? WHERE id = ?"
	if sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"ann@x.com", "Ann", 1}) {
		t.Errorf("args = %v", args)
	}
	if _, _, err := NewQuery[User](c).UpdateSQL(map[string]any{}); err == nil {
		t.Error("expected error for empty update")
	}
	if _, _, err := NewQuery[User](c).UpdateSQL(map[string]any{"nope": 1}); err == nil {
		t.Error("expected error for unknown column")
	}
}

func TestCreateSQL(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[User](c).CreateSQL(User{Name: "Zoe", Email: "z@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	// id is auto and therefore omitted.
	if sql != "INSERT INTO users (name, email) VALUES (?, ?)" {
		t.Errorf("sql = %q", sql)
	}
	if !reflect.DeepEqual(args, []any{"Zoe", "z@x.com"}) {
		t.Errorf("args = %v", args)
	}
}

func TestCreateManySQL(t *testing.T) {
	c := newTestClient(t)
	sql, args, err := NewQuery[User](c).CreateManySQL([]User{
		{Name: "A", Email: "a@x.com"},
		{Name: "B", Email: "b@x.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sql != "INSERT INTO users (name, email) VALUES (?, ?), (?, ?)" {
		t.Errorf("sql = %q", sql)
	}
	if !reflect.DeepEqual(args, []any{"A", "a@x.com", "B", "b@x.com"}) {
		t.Errorf("args = %v", args)
	}
	if _, _, err := NewQuery[User](c).CreateManySQL(nil); err == nil {
		t.Error("expected error for empty CreateMany")
	}
}

func TestDeferredErrors(t *testing.T) {
	c := newTestClient(t)
	if _, _, err := NewQuery[int](c).FindManySQL(); err == nil {
		t.Error("expected error for non-struct type")
	}
	// Include error is deferred to the terminal op.
	if _, err := NewQuery[Post](c).Include("Nope").Count(context.Background()); err == nil {
		t.Error("expected deferred include error")
	}
}

// --- Execution round-trips -------------------------------------------------

func TestFindManyExec(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	mem.rows = [][]driver.Value{
		{int64(1), "Alice", "alice@x.com"},
		{int64(2), "Bob", "bob@x.com"},
	}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	users, err := NewQuery[User](c).Where(Gt("id", 0)).FindMany(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Name != "Alice" || users[1].Email != "bob@x.com" {
		t.Fatalf("users = %+v", users)
	}
	if got := mem.lastQuery().query; got != "SELECT id, name, email FROM users WHERE id > ?" {
		t.Errorf("query = %q", got)
	}
}

func TestFindFirstExec(t *testing.T) {
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}

	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	mem.rows = [][]driver.Value{{int64(5), "Cara", "cara@x.com"}}
	u, err := NewQuery[User](c).Where(Equals("id", 5)).FindUnique(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != 5 || u.Name != "Cara" {
		t.Errorf("user = %+v", u)
	}

	mem.reset()
	mem.cols = []string{"id", "name", "email"}
	if _, err := NewQuery[User](c).Where(Equals("id", 999)).FindFirst(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestIncludeExec(t *testing.T) {
	mem.reset()
	mem.cols = []string{"id", "title", "author_id", "uid", "uname", "uemail"}
	mem.rows = [][]driver.Value{
		{int64(1), "Hello", int64(2), int64(2), "Bob", "bob@x.com"},
	}
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Register(Post{}); err != nil {
		t.Fatal(err)
	}
	posts, err := NewQuery[Post](c).Include("Author").FindMany(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("posts = %+v", posts)
	}
	p := posts[0]
	if p.Title != "Hello" || p.Author == nil || p.Author.Name != "Bob" {
		t.Errorf("post = %+v author = %+v", p, p.Author)
	}
}

func TestCreateExec(t *testing.T) {
	mem.reset()
	mem.lastInsertID = 42
	mem.rowsAffected = 1
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	u, err := NewQuery[User](c).Create(context.Background(), User{Name: "Zoe", Email: "z@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != 42 {
		t.Errorf("id = %d, want 42", u.ID)
	}
	q := mem.lastQuery()
	if q.query != "INSERT INTO users (name, email) VALUES (?, ?)" {
		t.Errorf("query = %q", q.query)
	}
	if !reflect.DeepEqual(q.args, []driver.Value{"Zoe", "z@x.com"}) {
		t.Errorf("args = %v", q.args)
	}
}

func TestMutationExec(t *testing.T) {
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db)
	if _, err := c.Register(User{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	mem.reset()
	mem.rowsAffected = 2
	n, err := NewQuery[User](c).CreateMany(ctx, []User{{Name: "A"}, {Name: "B"}})
	if err != nil || n != 2 {
		t.Fatalf("CreateMany n=%d err=%v", n, err)
	}

	mem.reset()
	mem.rowsAffected = 3
	n, err = NewQuery[User](c).Where(Gt("id", 1)).Update(ctx, map[string]any{"name": "X"})
	if err != nil || n != 3 {
		t.Fatalf("Update n=%d err=%v", n, err)
	}

	mem.reset()
	mem.rowsAffected = 4
	n, err = NewQuery[User](c).Where(Lt("id", 9)).Delete(ctx)
	if err != nil || n != 4 {
		t.Fatalf("Delete n=%d err=%v", n, err)
	}

	mem.reset()
	mem.countVal = 7
	cnt, err := NewQuery[User](c).Count(ctx)
	if err != nil || cnt != 7 {
		t.Fatalf("Count = %d err=%v", cnt, err)
	}
}

func TestNoDBConfigured(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	if _, err := NewQuery[User](c).FindMany(ctx); err == nil {
		t.Error("expected error with nil db (FindMany)")
	}
	if _, err := NewQuery[User](c).Count(ctx); err == nil {
		t.Error("expected error with nil db (Count)")
	}
	if _, err := NewQuery[User](c).Create(ctx, User{}); err == nil {
		t.Error("expected error with nil db (Create)")
	}
	if _, err := NewQuery[User](c).Where(Equals("id", 1)).Delete(ctx); err == nil {
		t.Error("expected error with nil db (Delete)")
	}
}

func TestClientAccessors(t *testing.T) {
	db := openMem(t)
	defer func() { _ = db.Close() }()
	c := NewClient(db, WithRegistry(NewRegistry()))
	if c.DB() != db {
		t.Error("DB accessor mismatch")
	}
	if c.Registry() == nil {
		t.Error("registry nil")
	}
}
