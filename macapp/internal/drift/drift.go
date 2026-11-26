// Package drift compares declarations copied from the Windows UI with their
// originals, so a change on one side cannot silently diverge (tests only).
package drift

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"testing"
)

// Decls returns the printed source of every top-level func, var, const and
// type in file, keyed by name (methods are "Recv.Name").
func Decls(t testing.TB, file string) map[string]string {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	out := map[string]string{}
	pr := func(n any) string {
		var b bytes.Buffer
		if err := printer.Fprint(&b, fs, n); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) == 1 {
				t := d.Recv.List[0].Type
				if s, ok := t.(*ast.StarExpr); ok {
					t = s.X
				}
				if id, ok := t.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			out[name] = pr(d)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.ValueSpec:
					for _, n := range s.Names {
						out[n.Name] = pr(s)
					}
				case *ast.TypeSpec:
					out[s.Name.Name] = pr(s)
				}
			}
		}
	}
	return out
}

// Same fails for every name whose declaration differs between ours and theirs.
func Same(t *testing.T, ours, theirs map[string]string, names ...string) {
	t.Helper()
	for _, n := range names {
		o, ok1 := ours[n]
		w, ok2 := theirs[n]
		switch {
		case !ok1:
			t.Errorf("%s: missing in the macOS copy", n)
		case !ok2:
			t.Errorf("%s: no longer in the Windows source", n)
		case o != w:
			t.Errorf("%s drifted from the Windows UI:\nmacOS:\n%s\nWindows:\n%s", n, o, w)
		}
	}
}
