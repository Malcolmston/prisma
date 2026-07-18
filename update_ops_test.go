package prisma

import (
	"reflect"
	"testing"
)

func TestUpdateOpsSQL(t *testing.T) {
	c := newTestClient(t)
	cases := []struct {
		name string
		data map[string]any
		sql  string
		args []any
	}{
		{
			"increment",
			map[string]any{"id": Increment(5)},
			"UPDATE users SET id = id + ? WHERE id = ?",
			[]any{5, int64(1)},
		},
		{
			"decrement",
			map[string]any{"id": Decrement(2)},
			"UPDATE users SET id = id - ? WHERE id = ?",
			[]any{2, int64(1)},
		},
		{
			"multiply",
			map[string]any{"id": Multiply(3)},
			"UPDATE users SET id = id * ? WHERE id = ?",
			[]any{3, int64(1)},
		},
		{
			"divide",
			map[string]any{"id": Divide(4)},
			"UPDATE users SET id = id / ? WHERE id = ?",
			[]any{4, int64(1)},
		},
		{
			"setto",
			map[string]any{"name": SetTo("Ada")},
			"UPDATE users SET name = ? WHERE id = ?",
			[]any{"Ada", int64(1)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sqlStr, args, err := NewQuery[User](c).
				Where(Equals("id", int64(1))).
				UpdateSQL(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if sqlStr != tc.sql {
				t.Errorf("sql = %q, want %q", sqlStr, tc.sql)
			}
			if !reflect.DeepEqual(args, tc.args) {
				t.Errorf("args = %v, want %v", args, tc.args)
			}
		})
	}
}

// TestUpdateOpsMixed checks that atomic ops and plain literals coexist and that
// the deterministic (sorted) column ordering places the atomic op's value and
// the WHERE arg in the expected positions.
func TestUpdateOpsMixed(t *testing.T) {
	c := newTestClient(t)
	sqlStr, args, err := NewQuery[User](c).
		Where(Equals("id", int64(7))).
		UpdateSQL(map[string]any{"email": "e@x.com", "id": Increment(1)})
	if err != nil {
		t.Fatal(err)
	}
	// Columns sort: email, id.
	want := "UPDATE users SET email = ?, id = id + ? WHERE id = ?"
	if sqlStr != want {
		t.Errorf("sql = %q, want %q", sqlStr, want)
	}
	if !reflect.DeepEqual(args, []any{"e@x.com", 1, int64(7)}) {
		t.Errorf("args = %v", args)
	}
}
