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
