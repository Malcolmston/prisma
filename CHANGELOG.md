# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/), and the project adheres to
semantic versioning.

## [0.2.0] - 2026-07-17

Large feature release moving toward Prisma parity. Still pure Go standard
library — no third-party dependencies, no cgo.

### Added

- **Relation filters**: `Some`, `Every`, `None` (to-many) and `Is` (to-one)
  compile to correlated `EXISTS` / `NOT EXISTS` subqueries and compose inside
  `And`/`Or`/`NotGroup`, including nesting through further relations.
- **Aggregations**: `Query.Aggregate()` with `Count`, `Sum`, `Avg`, `Min`,
  `Max`, returning a typed `AggregateResult`.
- **GroupBy**: `Query.GroupBy(cols...)` with the same aggregate functions plus
  `Having` filters; `Exec` returns one `map[string]any` per group.
- **Distinct**: `Query.Distinct(cols...)` — `SELECT DISTINCT`, or
  `DISTINCT ON (...)` on dialects that support it.
- **Cursor pagination**: `Query.Cursor(column, value)` keyset predicate keyed to
  the column's `OrderBy` direction, alongside existing `Take`/`Skip` offset.
- **NULLS-aware ordering**: `Query.OrderByNulls(column, dir, nulls)` with native
  `NULLS FIRST/LAST` on PostgreSQL and an emulated `IS NULL` sort elsewhere.
- **Nested writes**: `Query.NestedCreate(value)` with `Create`, `Connect`,
  `ConnectOrCreate` and `Update` on to-many relations, executed as one
  transaction with the parent key threaded into each child statement.
- **Transactions**: `Client.Transaction` (interactive) and `Client.Batch`
  (batched) over `*sql.Tx`, with queries started via `NewTxQuery`.
- **Raw queries**: `Client.QueryRaw`, `QueryRawInto[T]`, `Client.ExecuteRaw`
  (and `Tx` variants) with positional parameter binding.
- **Upsert**: `Query.Upsert` / `UpsertSQL` with `ON CONFLICT` and
  `ON DUPLICATE KEY UPDATE` styles.
- **createMany skip-duplicates**: `Query.SkipDuplicates()` →
  `ON CONFLICT DO NOTHING` / `INSERT IGNORE`.
- **updateMany / deleteMany**: `Query.UpdateMany` / `Query.DeleteMany` returning
  affected-row counts.
- **JSON path filters**: `JSONPath` / `JSONEquals` using dialect-appropriate
  extraction (`json_extract` / `#>>`).
- **Typed errors**: `Error` type with Prisma-style codes (`P2002`, `P2003`,
  `P2011`, `P2000`, `P2025`) and matching sentinels (`ErrUniqueViolation`, …);
  execution errors are mapped automatically.
- **MySQL dialect**: `prisma.MySQLDialect`.

### Changed

- Queries execute through an internal `execer` abstraction so the same builder
  runs against a `*sql.DB` or a `*sql.Tx`.

## [0.1.0]

- Initial release: model registry, generic chainable `Query[T]`, parameterized
  SQL generation with `*SQL` inspection methods, `Include` for to-one relations,
  `Question`/`Dollar` dialects, and struct scanning over `database/sql`.
