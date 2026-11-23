package declcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNumberEditDeclarativeValuesAreFloat64 guards a walk startup crash:
// NumberEdit.Value is a Property (interface{}), so Value: 1 is stored as
// int and walk's assertFloat64Or turns it into 0. SetValue(0) then fails
// with "value out of range" when MinValue > 0, which is how v1.10 failed
// to start (Frames per exposure). A float literal (1.0) or float64(...)
// is required. MinValue/MaxValue are float64 fields and are fine as ints.
func TestNumberEditDeclarativeValuesAreFloat64(t *testing.T) {
	var files []string
	root := ".."
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "third_party" || base == "vendor" || (strings.HasPrefix(base, ".") && base != "." && base != "..") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, ".bak") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var n int
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		ast.Inspect(f, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if !isIdent(lit.Type, "NumberEdit") {
				return true
			}
			n++
			var val, min, max ast.Expr
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch name.Name {
				case "Value":
					val = kv.Value
				case "MinValue":
					min = kv.Value
				case "MaxValue":
					max = kv.Value
				}
			}
			pos := fset.Position(lit.Pos())
			if val == nil {
				return true
			}
			kind, fv, ok := numberEditValue(val)
			if !ok {
				t.Errorf("%s: NumberEdit.Value is %s; use a float64 literal (1.0) or float64(...). An int is stored as interface{} and walk applies 0, which crashes when MinValue > 0", pos, exprString(val))
				return true
			}
			if kind != "float" {
				return true
			}
			lo, lok := constFloat(min)
			hi, hok := constFloat(max)
			if lok && hok && lo > hi {
				t.Errorf("%s: NumberEdit MinValue %v > MaxValue %v", pos, lo, hi)
			}
			if lok && hok && lo != hi && (fv < lo || fv > hi) {
				t.Errorf("%s: NumberEdit Value %v is outside [%v, %v]", pos, fv, lo, hi)
			}
			return true
		})
	}
	if n < 8 {
		t.Fatalf("expected to find the app NumberEdit declarations, found %d", n)
	}
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func numberEditValue(e ast.Expr) (kind string, v float64, ok bool) {
	switch t := e.(type) {
	case *ast.BasicLit:
		if t.Kind == token.FLOAT {
			f, err := strconv.ParseFloat(t.Value, 64)
			return "float", f, err == nil
		}
		return "", 0, false
	case *ast.UnaryExpr:
		if t.Op == token.SUB {
			kind, v, ok = numberEditValue(t.X)
			return kind, -v, ok
		}
	case *ast.CallExpr:
		if isIdent(t.Fun, "float64") && len(t.Args) == 1 {
			if f, ok := constFloat(t.Args[0]); ok {
				return "float", f, true
			}
			return "call", 0, true
		}
	case *ast.Ident, *ast.SelectorExpr:
		// runtime float64 (e.g. p.Def); type is checked at compile time
		return "ident", 0, true
	}
	return "", 0, false
}

func constFloat(e ast.Expr) (float64, bool) {
	if e == nil {
		return 0, false
	}
	switch t := e.(type) {
	case *ast.BasicLit:
		if t.Kind == token.INT || t.Kind == token.FLOAT {
			f, err := strconv.ParseFloat(t.Value, 64)
			return f, err == nil
		}
	case *ast.UnaryExpr:
		if t.Op == token.SUB {
			if f, ok := constFloat(t.X); ok {
				return -f, true
			}
		}
	case *ast.Ident:
		// untyped consts such as framesPerExpMin are folded only when
		// written as a basic literal; treat named consts as unknown.
	}
	return 0, false
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.BasicLit:
		return t.Value + " (" + t.Kind.String() + ")"
	case *ast.UnaryExpr:
		return t.Op.String() + exprString(t.X)
	default:
		return "non-float64 expression"
	}
}
