package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestMainGoIsOnlyCompositionRoot(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Imports) != 1 {
		t.Fatalf("main.go imports %d packages, want only internal/app", len(file.Imports))
	}
	path, err := strconv.Unquote(file.Imports[0].Path.Value)
	if err != nil {
		t.Fatal(err)
	}
	if path != "github.com/charle-z/mcp-devbox/internal/app" {
		t.Fatalf("main.go imports %q, want internal/app", path)
	}

	functions := 0
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		functions++
		if function.Name.Name != "main" {
			t.Errorf("unexpected function %s in composition root", function.Name.Name)
			continue
		}
		if function.Recv != nil || function.Type.Params.NumFields() != 0 || len(function.Body.List) != 1 {
			t.Error("main must be a single parameterless delegation")
			continue
		}
		expression, ok := function.Body.List[0].(*ast.ExprStmt)
		if !ok {
			t.Error("main body must contain one call")
			continue
		}
		call, ok := expression.X.(*ast.CallExpr)
		if !ok {
			t.Error("main body must contain one call")
			continue
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Main" || len(call.Args) != 0 {
			t.Error("main must delegate exactly to app.Main()")
		}
	}
	if functions != 1 {
		t.Fatalf("main.go contains %d functions, want exactly main", functions)
	}
}
