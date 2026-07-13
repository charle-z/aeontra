package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceFacadeContainsOnlyDelegatingConfigurationMethods(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test path")
	}
	dir := filepath.Dir(currentFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"WithActionPlanStore":  true,
		"WithRunner":           true,
		"WithSandboxRunner":    true,
		"WithTestCommand":      true,
		"WithCoolify":          true,
		"WithGitHub":           true,
		"WithValidationRunner": true,
		"WithPrivilegedConfig": true,
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || receiverName(function) != "Service" {
				continue
			}
			if !allowed[function.Name.Name] {
				t.Errorf("operational method %s must live on a capability, not Service", function.Name.Name)
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				assignment, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, target := range assignment.Lhs {
					selector, ok := target.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					identifier, ok := selector.X.(*ast.Ident)
					if ok && identifier.Name == "s" {
						t.Errorf("Service.%s mutates %s directly; delegate to the owning capability/core", function.Name.Name, selector.Sel.Name)
					}
				}
				return true
			})
		}
	}
}

func receiverName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return ""
	}
	typeExpr := function.Recv.List[0].Type
	if pointer, ok := typeExpr.(*ast.StarExpr); ok {
		typeExpr = pointer.X
	}
	identifier, _ := typeExpr.(*ast.Ident)
	if identifier == nil {
		return ""
	}
	return identifier.Name
}
