package prisma

import "testing"

// BenchmarkRawConditionCompile measures the placeholder-substitution path used
// by RawCondition/WhereRaw, which does per-call string splitting and binding.
func BenchmarkRawConditionCompile(b *testing.B) {
	cl := NewClient(nil)
	m, _ := cl.Register(User{})
	cond := RawCondition("a = ? AND b = ? OR c = ?", 1, 2, 3)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w := &sqlWriter{d: Question, reg: cl.registry, base: m}
		_ = cond.compile(w)
	}
}

// BenchmarkUpdateOpsSQL measures building an UPDATE with an atomic operation.
func BenchmarkUpdateOpsSQL(b *testing.B) {
	c := NewClient(nil)
	if _, err := c.Register(User{}); err != nil {
		b.Fatal(err)
	}
	data := map[string]any{"id": Increment(1), "email": "e@x.com"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := NewQuery[User](c).Where(Equals("id", int64(1))).UpdateSQL(data); err != nil {
			b.Fatal(err)
		}
	}
}
