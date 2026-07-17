// Library content for the prisma documentation site. Mirrors the shape used by
// the malcolmston/go landing site's data.ts so the sibling sites stay in sync.
export interface Lib {
  id: string; name: string; icon: string; accent: string; pkg: string; node: string;
  repo: string; docs: string; tagline: string; blurb: string; tags: string[];
  features: string[]; node_code: string; go_code: string; integrate: string;
}

export const NODE_ACCENT = '#8cc84b';

export const PRISMA: Lib = {
  id:"prisma", name:"Prisma", icon:'<i class="fa-solid fa-database"></i>', accent:"#5a67d8",
  pkg:"github.com/malcolmston/prisma", node:"prisma/prisma",
  repo:"https://github.com/malcolmston/prisma", docs:"https://malcolmston.github.io/prisma/",
  tagline:"Type-safe, stdlib-only ORM and query builder for Go.",
  blurb:"A small, type-safe query builder and lightweight ORM over the standard database/sql package, "+
    "inspired by the ergonomics of Prisma — with no dependencies beyond the Go standard library and no cgo. "+
    "Models are plain Go structs annotated with prisma:\"...\" struct tags and compiled into a Model by "+
    "reflection; a generic, chainable Query[T] builder emits parameterized SQL where literal values are always "+
    "bound as arguments and never concatenated into the statement. Every terminal operation has a matching "+
    "*SQL twin that returns the exact (sql, args) without touching the database, and a pluggable Dialect "+
    "switches placeholder style between ? (MySQL/SQLite) and $1, $2, … (PostgreSQL). The import path is "+
    "github.com/malcolmston/prisma and the package is named prisma.",
  tags:["type-safe","query builder","ORM","database/sql","generic Query[T]","parameterized SQL","struct tags","pluggable dialects"],
  features:[
    "Plain-struct models — annotate fields with <code>prisma:\"col=…,pk,auto\"</code> tags; reflection compiles them into a <code>Model</code> held by a <code>Registry</code>",
    "Generic chainable builder — <code>NewQuery[T]</code> with <code>Where</code>, <code>OrderBy</code>, <code>Take</code>, <code>Skip</code>, <code>Select</code> and <code>Include</code>",
    "Typed operators — <code>Equals</code>, <code>Not</code>, <code>In</code>, <code>NotIn</code>, <code>Lt</code>/<code>Lte</code>/<code>Gt</code>/<code>Gte</code>, <code>Contains</code>, <code>StartsWith</code>, <code>EndsWith</code>, combined with <code>And</code>, <code>Or</code> and <code>NotGroup</code>",
    "Always parameterized — literal values are bound as arguments, never concatenated into the SQL text",
    "<code>*SQL</code> twins — <code>FindManySQL</code>, <code>CountSQL</code>, <code>CreateSQL</code>, <code>UpdateSQL</code> and <code>DeleteSQL</code> return <code>(sql, args)</code> without hitting the database",
    "Terminal operations — <code>FindMany</code>, <code>FindFirst</code>, <code>FindUnique</code>, <code>Count</code>, <code>Create</code>, <code>CreateMany</code>, <code>Update</code> and <code>Delete</code>",
    "Eager loading — <code>Include</code> pulls a to-one relation via a LEFT JOIN and scans it into the struct",
    "Pluggable dialects — <code>Question</code> (<code>?</code>) and <code>Dollar</code> (<code>$1, $2, …</code>) selected with <code>WithDialect</code>",
    "Zero dependencies — pure Go standard library over <code>database/sql</code>, no cgo, nothing to audit but the toolchain"
  ],
  node_code:
`import { PrismaClient } from '@prisma/client'

const prisma = new PrismaClient()

const users = await prisma.user.findMany({
  where: { email: { startsWith: 'ada' }, id: { gt: 0 } },
  orderBy: { name: 'asc' },
  take: 10,
})

await prisma.user.create({
  data: { name: 'Ada', email: 'ada@example.com' },
})`,
  go_code:
`import "github.com/malcolmston/prisma"

client := prisma.NewClient(db) // any *sql.DB driver
client.Register(User{})

users, _ := prisma.NewQuery[User](client).
	Where(prisma.StartsWith("email", "ada"), prisma.Gt("id", 0)).
	OrderBy("name", prisma.Asc).
	Take(10).
	FindMany(ctx)

prisma.NewQuery[User](client).
	Create(ctx, User{Name: "Ada", Email: "ada@example.com"})

n, _ := prisma.NewQuery[User](client).Count(ctx)`,
  integrate:
`<span class="tok-c">// Every terminal op has a *SQL twin that returns the exact statement</span>
<span class="tok-c">// and argument slice without ever touching the database.</span>
sqlStr, args, _ := prisma.NewQuery[User](client).
	Where(prisma.Equals("email", "a@b.com"), prisma.Gt("id", 10)).
	OrderBy("name", prisma.Asc).
	Take(5).
	FindManySQL()
<span class="tok-c">// SELECT id, name, email FROM users WHERE email = ? AND id > ? ORDER BY name ASC LIMIT ?</span>

<span class="tok-c">// Eager-load a to-one relation via LEFT JOIN with Include.</span>
posts, _ := prisma.NewQuery[Post](client).
	Include("Author").
	Where(prisma.In("author_id", 1, 2, 3)).
	FindMany(ctx)

<span class="tok-c">// Switch to PostgreSQL $1,$2 placeholders with a pluggable dialect.</span>
pg := prisma.NewClient(db, prisma.WithDialect(prisma.Dollar))
prisma.NewQuery[User](pg).
	Where(prisma.Equals("id", 1)).
	Update(ctx, map[string]any{"name": "Ada Lovelace"})`
};
