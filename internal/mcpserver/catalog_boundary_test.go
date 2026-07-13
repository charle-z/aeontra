package mcpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestToolsGoContainsOnlyCatalogAdaptersAndComposition(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	toolsPath := filepath.Join(filepath.Dir(testFile), "tools.go")
	file, err := parser.ParseFile(token.NewFileSet(), toolsPath, nil, 0)
	if err != nil {
		t.Fatalf("parse tools.go: %v", err)
	}

	legacyHelpers := map[string]bool{
		"object":     true,
		"strProp":    true,
		"strArrProp": true,
		"boolProp":   true,
		"intProp":    true,
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if function.Recv == nil && legacyHelpers[function.Name.Name] {
			t.Errorf("legacy schema helper %s must live in catalog", function.Name.Name)
		}
		if function.Recv != nil && function.Name.Name == "add" {
			t.Error("direct Server.add registration must not exist after catalog modularization")
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "add" {
			t.Error("tools.go must not call Server.add directly")
		}
		return true
	})
}
