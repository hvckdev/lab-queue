package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A service is an independent module, with its own entry point and composition
// root. Shared implementation may come from platform, never another service.
func TestIndependentServiceProjects(t *testing.T) {
	for _, service := range []string{"accounts", "queue", "web"} {
		root := filepath.Join("..", "..", "projects", service)
		module := "labqueue/projects/" + service
		source, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(source), "module "+module+"\n") {
			t.Errorf("%s has no dedicated module", service)
		}
		for _, path := range []string{"cmd/main.go", "internal/app.go"} {
			if _, err := os.Stat(filepath.Join(root, path)); err != nil {
				t.Errorf("%s missing %s: %v", service, path, err)
			}
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, item := range file.Imports {
				dependency, _ := strconv.Unquote(item.Path.Value)
				if strings.HasPrefix(dependency, "labqueue/") && !strings.HasPrefix(dependency, module+"/") && dependency != "labqueue/pkg/platform" && !strings.HasPrefix(dependency, "labqueue/pkg/platform/") {
					t.Errorf("%s: service must not import another project: %s", path, dependency)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
