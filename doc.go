// Package prisma is a small, type-safe query builder and lightweight ORM over
// the standard database/sql package, inspired by the ergonomics of Prisma. It
// has no dependencies beyond the Go standard library.
//
// # Overview
//
// There are three layers:
//
//   - The model layer compiles a Go struct into a [Model] using reflection and
//     struct tags. A [Registry] stores compiled models and resolves them by
//     type or name.
//   - The builder layer is a generic, chainable [Query] that produces
//     parameterized SQL. Literal values are always bound as arguments and never
//     concatenated into the SQL text.
//   - The runtime layer executes queries against a *sql.DB through a [Client]
//     and scans result rows back into structs.
//
// # Defining a model
//
// Models are ordinary structs annotated with the "prisma" struct tag:
//
//	type User struct {
//	    ID    int64  `prisma:"col=id,pk,auto"`
//	    Name  string `prisma:"col=name"`
//	    Email string `prisma:"col=email"`
//	    Posts []Post `prisma:"relation,fk=author_id,references=id"`
//	}
//
//	type Post struct {
//	    ID       int64  `prisma:"col=id,pk,auto"`
//	    Title    string `prisma:"col=title"`
//	    AuthorID int64  `prisma:"col=author_id"`
//	    Author   *User  `prisma:"relation,fk=author_id,references=id"`
//	}
//
// Recognized tag options are documented on [Registry.Register]. Column and table
// names default to snake_case (tables are also pluralized) unless overridden by
// a tag or a [TableNamer] implementation.
//
// # Building queries
//
// Create a [Client], register the models, then start a query with the generic
// [NewQuery] constructor:
//
//	client := prisma.NewClient(db)
//	client.Register(User{})
//
//	q := prisma.NewQuery[User](client).
//	    Where(prisma.Equals("email", "a@b.com"), prisma.Gt("id", 10)).
//	    OrderBy("name", prisma.Asc).
//	    Take(20)
//
// Predicates are built with the operator helpers ([Equals], [Not], [In], [Lt],
// [Lte], [Gt], [Gte], [Contains], [StartsWith], [EndsWith]) and combined with
// [And], [Or] and [NotGroup].
//
// # SQL generation
//
// Every terminal operation has a matching *SQL method that returns the exact
// statement and argument slice without touching the database, which makes the
// output easy to assert in tests:
//
//	sqlStr, args, _ := q.FindManySQL()
//	// SELECT id, name, email FROM users WHERE email = ? AND id > ?
//	//   ORDER BY name ASC LIMIT ?
//	// args: ["a@b.com", 10, 20]
//
// The available terminal operations are [Query.FindMany], [Query.FindFirst],
// [Query.FindUnique], [Query.Count], [Query.Create], [Query.CreateMany],
// [Query.Update] and [Query.Delete].
//
// # Execution
//
// The executing methods run the generated SQL against the client's *sql.DB and,
// for reads, scan each row into a struct (including a to-one relation loaded via
// [Query.Include]). Placeholder style is controlled by the [Dialect]: [Question]
// ("?") is the default and [Dollar] ("$1", "$2", ...) targets PostgreSQL.
package prisma
