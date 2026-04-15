package dependencycheck_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRootModuleDoesNotDependOnSentry(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))

	cmd := exec.Command("go", "list", "-deps", "./...")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}
	for _, dep := range strings.Split(string(out), "\n") {
		if strings.Contains(dep, "github.com/getsentry") || strings.Contains(dep, "sentry-go") {
			t.Fatalf("root module unexpectedly depends on Sentry package %q", dep)
		}
	}
}
