package prisma

import "fmt"

// Example builds a query and prints the parameterized SQL and arguments it
// generates, without touching a database.
func Example() {
	client := NewClient(nil)
	if _, err := client.Register(User{}); err != nil {
		panic(err)
	}

	q := NewQuery[User](client).
		Where(Equals("email", "a@b.com"), Gt("id", 10)).
		OrderBy("name", Asc).
		Take(5)

	sqlStr, args, err := q.FindManySQL()
	if err != nil {
		panic(err)
	}
	fmt.Println(sqlStr)
	fmt.Println(args...)

	// Output:
	// SELECT id, name, email FROM users WHERE email = ? AND id > ? ORDER BY name ASC LIMIT ?
	// a@b.com 10 5
}

// ExampleSome shows a relation filter compiling to a correlated EXISTS
// subquery: users who have at least one published post.
func ExampleSome() {
	client := NewClient(nil)
	if _, err := client.Register(User{}); err != nil {
		panic(err)
	}
	if _, err := client.Register(Post{}); err != nil {
		panic(err)
	}

	sqlStr, args, err := NewQuery[User](client).
		Where(Some("Posts", Equals("title", "Hello"))).
		FindManySQL()
	if err != nil {
		panic(err)
	}
	fmt.Println(sqlStr)
	fmt.Println(args...)

	// Output:
	// SELECT id, name, email FROM users WHERE EXISTS (SELECT 1 FROM posts WHERE posts.author_id = users.id AND title = ?)
	// Hello
}

// ExampleQuery_Aggregate shows building an aggregation over a filtered set.
func ExampleQuery_Aggregate() {
	client := NewClient(nil)
	if _, err := client.Register(Order{}); err != nil {
		panic(err)
	}

	sqlStr, args, err := NewQuery[Order](client).
		Where(Equals("status", "paid")).
		Aggregate().Count().Sum("amount").Avg("amount").
		SQL()
	if err != nil {
		panic(err)
	}
	fmt.Println(sqlStr)
	fmt.Println(args...)

	// Output:
	// SELECT COUNT(*) AS _count, SUM(amount) AS _sum_amount, AVG(amount) AS _avg_amount FROM orders WHERE status = ?
	// paid
}

// ExampleQuery_UpsertSQL shows an insert-or-update statement.
func ExampleQuery_UpsertSQL() {
	client := NewClient(nil)
	if _, err := client.Register(User{}); err != nil {
		panic(err)
	}

	sqlStr, args, err := NewQuery[User](client).UpsertSQL(
		[]string{"email"},
		User{Name: "Ada", Email: "ada@example.com"},
		map[string]any{"name": "Ada Lovelace"},
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(sqlStr)
	fmt.Println(args...)

	// Output:
	// INSERT INTO users (name, email) VALUES (?, ?) ON CONFLICT (email) DO UPDATE SET name = ?
	// Ada ada@example.com Ada Lovelace
}
