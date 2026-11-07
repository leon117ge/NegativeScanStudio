//go:build ignore

package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

func grab(file string, names map[string]string, out *bytes.Buffer) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, file, nil, 0)
	if err != nil {
		panic(err)
	}
	src, _ := os.ReadFile(file)
	for _, d := range f.Decls {
		var name string
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Recv == nil {
				name = x.Name.Name
			}
		case *ast.GenDecl:
			if x.Tok == token.TYPE {
				name = x.Specs[0].(*ast.TypeSpec).Name.Name
			}
		}
		if nn, ok := names[name]; ok {
			s := string(src[fs.Position(d.Pos()).Offset:fs.Position(d.End()).Offset])
			if nn != name {
				s = strings.Replace(s, name+"(", nn+"(", 1)
			}
			out.WriteString(s + "\n\n")
		}
	}
}

func main() {
	var b bytes.Buffer
	b.WriteString("package main\n\nimport (\n\"fmt\"\n\"image\"\n_ \"image/png\"\n\"os\"\n\"path/filepath\"\n\"sort\"\n\"strings\"\n\"basetool/basestat\"\n\"golang.org/x/image/tiff\"\n)\nvar _ = sort.Float64s\n\n")
	grab("./main.go", map[string]string{"Row": "Row", "getter": "getter", "measure": "measure", "measureRegion": "measureRegion", "measureRow": "measureRow", "median": "median"}, &b)
	grab("./wb.go", map[string]string{"decodeImage": "decodeImage"}, &b)
	grab("./main.go.bak_v05", map[string]string{"measure": "measureOld", "median": "medianOld"}, &b)
	o := b.String()
	// old measure calls median( -> medianOld(
	i := strings.Index(o, "func measureOld(")
	o = o[:i] + strings.ReplaceAll(o[i:], "median(", "medianOld(")
	o = strings.Replace(o, "func medianOld(", "func medianOld(", 1)
	res, err := format.Source([]byte(o))
	if err != nil {
		os.WriteFile("m1_gen.go", []byte(o), 0o644)
		panic(err)
	}
	os.WriteFile("m1_gen.go", res, 0o644)
}
