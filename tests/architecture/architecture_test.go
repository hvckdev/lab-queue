package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These dependency rules protect the requested layering during later changes.
// Composition roots and tests may wire concrete implementations together.
func TestServiceLayerBoundaries(t *testing.T) {
	root := filepath.Join("..", "..", "projects")
	for _, service := range []string{"accounts", "queue", "web"} {
		for _, layer := range []string{"model", "handler", "usecase", "service", "adapter"} {
			path := filepath.Join(root, service, "internal", layer)
			files := 0
			err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				files++
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
				if err != nil {
					return err
				}
				for _, item := range file.Imports {
					dependency, _ := strconv.Unquote(item.Path.Value)
					if forbidden(service, layer, dependency) {
						t.Errorf("%s: %s layer must not import %s", path, layer, dependency)
					}
				}
				if layer == "model" {
					ast.Inspect(file, func(node ast.Node) bool {
						field, ok := node.(*ast.Field)
						if ok && field.Tag != nil && strings.Contains(field.Tag.Value, "json:") {
							t.Errorf("%s: transport/persistence JSON tags belong to outer DTOs", path)
						}
						return true
					})
				}
				return nil
			})
			if err != nil {
				t.Errorf("%s: %v", path, err)
			}
			if files == 0 {
				t.Errorf("%s layer has no implementation", path)
			}
		}
	}
}

func forbidden(service, layer, dependency string) bool {
	base := "labqueue/projects/" + service + "/internal/"
	if layer == "model" || layer == "service" || layer == "usecase" {
		if dependency == "net/http" || strings.HasPrefix(dependency, "net/http/") || dependency == "database/sql" || dependency == "encoding/json" {
			return true
		}
		if strings.HasPrefix(dependency, "github.com/") || strings.HasPrefix(dependency, "golang.org/x/") || strings.HasPrefix(dependency, "labqueue/pkg/platform") {
			return true
		}
		if strings.HasPrefix(dependency, "labqueue/") {
			allowed := strings.HasPrefix(dependency, base+"model") || layer == "usecase" && strings.HasPrefix(dependency, base+"service")
			return !allowed
		}
	}
	if layer == "handler" {
		return strings.HasPrefix(dependency, base+"adapter") || strings.HasPrefix(dependency, base+"service") || dependency == "database/sql" || strings.Contains(dependency, "pgx") || strings.HasPrefix(dependency, "labqueue/pkg/platform/infra")
	}
	if layer == "adapter" {
		return strings.HasPrefix(dependency, base+"handler")
	}
	return false
}

func TestSharedInfraDoesNotDependOnServices(t *testing.T) {
	err := filepath.WalkDir(filepath.Join("..", "..", "pkg", "platform", "infra"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, item := range file.Imports {
			dependency, _ := strconv.Unquote(item.Path.Value)
			if strings.HasPrefix(dependency, "labqueue/") && !strings.HasPrefix(dependency, "labqueue/pkg/platform/") {
				t.Errorf("%s: infrastructure must not import service package %s", path, dependency)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
