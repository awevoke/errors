# Optional Sentry Module Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the default `github.com/cockroachdb/errors` module usable without importing or requiring `github.com/getsentry/sentry-go`, while preserving Sentry reporting through an explicit optional module.

**Architecture:** Move all Sentry-native APIs, stacktrace conversion, report construction, and Sentry tests into a nested module at `sentry/` with module path `github.com/cockroachdb/errors/sentry`. The root module keeps Sentry-neutral error construction, wrapping, formatting, stack capture, safe details, protobuf, and gRPC behavior. Root tests verify the root module dependency graph contains no `getsentry/sentry-go`; nested module tests verify Sentry behavior still works.

**Tech Stack:** Go modules, nested module, `github.com/getsentry/sentry-go`, existing `github.com/cockroachdb/errors` packages.

**Execution Constraints:** Use the existing branch and workspace. Do not create a new git worktree. Execute exactly one task at a time; do not batch or group tasks even if they appear related.

**Required Review Workflow:** Use `superpowers:subagent-driven-development` while executing this plan. After every task, before committing, launch subagents for spec review and code review of that task's changes. Fix any confirmed issues before moving to the next task. Do not skip review checkpoints.

---

### Task 1: Add Root Dependency Boundary Test

**Files:**
- Create: `internal/dependencycheck/sentry_test.go`

**Step 1: Write failing root dependency test**

Create a test that runs from the root module and verifies the root package dependency graph does not contain Sentry:

```go
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
```

**Step 2: Run the test and verify it fails**

Run:

```sh
go test ./internal/dependencycheck
```

Expected: FAIL because the root module currently imports `github.com/getsentry/sentry-go`.

**Step 3: Commit**

```sh
git add internal/dependencycheck/sentry_test.go
git commit -m "test: guard root module against sentry dependency"
```

### Task 2: Create Optional Sentry Module Skeleton

**Files:**
- Create: `sentry/go.mod`
- Create: `sentry/doc.go`

**Step 1: Add nested module**

Create `sentry/go.mod`:

```go
module github.com/cockroachdb/errors/sentry

go 1.23.0

toolchain go1.23.8

require (
	github.com/cockroachdb/errors v0.0.0
	github.com/getsentry/sentry-go v0.27.0
)

replace github.com/cockroachdb/errors => ..
```

Use `go mod tidy` inside `sentry/` after code is moved.

**Step 2: Add package docs**

Create `sentry/doc.go`:

```go
// Package sentry provides optional Sentry reporting support for
// github.com/cockroachdb/errors.
//
// This package intentionally lives in a separate module so default users of
// github.com/cockroachdb/errors do not import or require sentry-go.
package sentry
```

**Step 3: Run nested module list**

Run:

```sh
(cd sentry && go list ./...)
```

Expected: PASS with only the empty package.

**Step 4: Commit**

```sh
git add sentry/go.mod sentry/doc.go
git commit -m "sentry: add optional sentry module"
```

### Task 3: Move Stacktrace Conversion Into Optional Module

**Files:**
- Move: `withstack/reportable.go` -> `sentry/stacktrace.go`
- Move: `withstack/reportable_test.go` -> `sentry/stacktrace_test.go`
- Modify: `withstack_api.go`
- Modify: `withstack/one_line_source_test.go` if fixture names need updates

**Step 1: Move implementation**

Move Sentry-native stacktrace conversion from `withstack/reportable.go` into `sentry/stacktrace.go`.

Set package to `sentry`. Import root packages by full module path:

```go
import (
	"github.com/cockroachdb/errors/errbase"
	"github.com/cockroachdb/errors/withstack"
	sentrygo "github.com/getsentry/sentry-go"
	pkgErr "github.com/pkg/errors"
)
```

Avoid naming the imported Sentry package `sentry` inside package `sentry`; use `sentrygo`.

Expose:

```go
type ReportableStackTrace = sentrygo.Stacktrace

func GetReportableStackTrace(err error) *ReportableStackTrace
```

When computing `ourWithStackName`, call `withstack.WithStack(err)` from the root module.

**Step 2: Remove root API wrappers**

Delete from `withstack_api.go`:

```go
type ReportableStackTrace = withstack.ReportableStackTrace
func GetReportableStackTrace(err error) *ReportableStackTrace
```

Keep `WithStack`, `WithStackDepth`, and `GetOneLineSource`.

**Step 3: Move tests**

Move `withstack/reportable_test.go` into `sentry/stacktrace_test.go`.

Change imports:

```go
errorssentry "github.com/cockroachdb/errors/sentry"
```

Replace:

```go
withstack.GetReportableStackTrace(err)
```

with:

```go
errorssentry.GetReportableStackTrace(err)
```

Keep root `withstack` imports for constructing errors.

**Step 4: Run tests**

Run:

```sh
go test ./withstack
(cd sentry && go test ./...)
```

Expected: root `withstack` tests pass; nested Sentry stacktrace tests pass.

**Step 5: Commit**

```sh
git add withstack_api.go withstack sentry
git commit -m "sentry: move reportable stacktrace conversion"
```

### Task 4: Move Report Package Into Optional Module

**Files:**
- Move: `report/report.go` -> `sentry/report.go`
- Move: `report/reportables.go` -> `sentry/reportables.go`
- Move: `report/report_test.go` -> `sentry/report_test.go`
- Modify: `fmttests/datadriven_test.go`
- Delete: `report/`
- Delete: `report_api.go`

**Step 1: Move report implementation**

Move `report/report.go` into `sentry/report.go` and set package to `sentry`.

Update imports:

- Keep root imports such as `github.com/cockroachdb/errors/domains`.
- Replace `github.com/cockroachdb/errors/withstack` reportable stack calls with local package calls.
- Import Sentry as `sentrygo "github.com/getsentry/sentry-go"`.

Rename exported functions:

```go
func BuildReport(err error) (event *sentrygo.Event, extraDetails map[string]interface{})
func ReportError(err error) (eventID string)
```

Do not keep root-level `errors.BuildSentryReport` or `errors.ReportError` wrappers in the root module.

**Step 2: Keep compatibility aliases only inside optional module if desired**

Inside `sentry/report.go`, optional aliases are acceptable because they do not affect root dependencies:

```go
func BuildSentryReport(err error) (*sentrygo.Event, map[string]interface{}) {
	return BuildReport(err)
}
```

Do not add aliases in the root package.

**Step 3: Move reportables**

Move `report/reportables.go` into `sentry/reportables.go`, update package to `sentry`, and use `sentrygo.Stacktrace`.

**Step 4: Move report tests**

Move `report/report_test.go` to `sentry/report_test.go`.

Change package and imports:

```go
package sentry_test

errorssentry "github.com/cockroachdb/errors/sentry"
sentrygo "github.com/getsentry/sentry-go"
```

Replace:

```go
report.ReportError(err)
```

with:

```go
errorssentry.ReportError(err)
```

**Step 5: Delete root report API**

Delete `report_api.go`. Delete empty `report/` directory after moves.

**Step 6: Remove root formatting-test Sentry hooks**

In `fmttests/datadriven_test.go`, remove:

```go
"github.com/cockroachdb/errors/report"
"github.com/getsentry/sentry-go"
```

Remove root-test code paths that construct Sentry clients, intercepting transports, and `===== Sentry reporting` output sections.

Keep root `fmttests` coverage for:

- `%s`, `%v`, `%+v`, `%#v`, `%x`, `%X` formatting
- protobuf encode/decode formatting
- safe detail formatting
- domains, telemetry keys, hints, details, assertions, barriers, secondary errors

This keeps the root module buildable immediately after `report/` and `report_api.go` are removed.

**Step 7: Run tests**

Run:

```sh
go test ./...
(cd sentry && go test ./...)
```

Expected: root tests pass without any Sentry imports; nested Sentry tests pass or expose import updates needed before committing.

**Step 8: Commit**

```sh
git add -A report report_api.go sentry fmttests/datadriven_test.go
git commit -m "sentry: move reporting package to optional module"
```

### Task 5: Restore Optional Sentry Datadriven Coverage

**Files:**
- Create: `sentry/fmttests/datadriven_test.go`
- Move or copy: selected Sentry fixture data from `fmttests/testdata/format/*` to `sentry/fmttests/testdata/report/*`

**Step 1: Create optional-module formatting/report tests**

Create `sentry/fmttests/datadriven_test.go` by copying the Sentry-specific portions of the current datadriven test harness.

Use imports from the nested module:

```go
errorssentry "github.com/cockroachdb/errors/sentry"
sentrygo "github.com/getsentry/sentry-go"
```

The optional-module test should build representative errors using root packages and verify Sentry report output.

**Step 2: Decide fixture scope**

Do not copy every root formatting fixture unless necessary. Start with representative Sentry report cases covering:

- leaf error with safe details
- wrapper chain with stack
- domain tag
- context tags
- telemetry keys
- hint/detail behavior
- assertion failure
- encoded/decoded remote error

Place fixtures under:

```text
sentry/fmttests/testdata/report/
```

**Step 3: Run root and nested tests**

Run:

```sh
go test ./fmttests
(cd sentry && go test ./...)
```

Expected: root formatting tests pass without Sentry imports; optional Sentry tests pass with Sentry imports.

**Step 4: Commit**

```sh
git add fmttests sentry/fmttests
git commit -m "sentry: split sentry report fixtures from formatting tests"
```

### Task 6: Remove Root Sentry Dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `sentry/go.mod`
- Modify: `sentry/go.sum`

**Step 1: Tidy root module**

Run:

```sh
go mod tidy
```

Expected: root `go.mod` no longer requires `github.com/getsentry/sentry-go`.

**Step 2: Tidy optional module**

Run:

```sh
(cd sentry && go mod tidy)
```

Expected: `sentry/go.mod` requires `github.com/getsentry/sentry-go`.

**Step 3: Verify dependency boundary**

Run:

```sh
go list -deps ./... | rg "github.com/getsentry|sentry-go"
```

Expected: no matches.

Run:

```sh
(cd sentry && go list -deps ./... | rg "github.com/getsentry/sentry-go")
```

Expected: matches `github.com/getsentry/sentry-go`.

**Step 4: Run tests**

Run:

```sh
go test ./...
(cd sentry && go test ./...)
```

Expected: PASS.

**Step 5: Commit**

```sh
git add go.mod go.sum sentry/go.mod sentry/go.sum
git commit -m "mod: make sentry dependency optional"
```

### Task 7: Update Documentation

**Files:**
- Modify: `README.md`
- Modify: package comments if present

**Step 1: Update root README**

Replace root API references:

```go
errors.BuildSentryReport(...)
errors.ReportError(...)
errors.GetReportableStackTrace(...)
```

with optional-module references:

```go
errorssentry.BuildReport(...)
errorssentry.ReportError(...)
errorssentry.GetReportableStackTrace(...)
```

Add a clear note:

```text
Sentry support lives in the optional module github.com/cockroachdb/errors/sentry.
Importing github.com/cockroachdb/errors alone does not require sentry-go.
```

**Step 2: Update dependency docs**

If the README feature table mentions Sentry, mark it as optional module support.

**Step 3: Run doc-related checks**

Run:

```sh
go test ./...
(cd sentry && go test ./...)
```

Expected: PASS.

**Step 4: Commit**

```sh
git add README.md
git commit -m "docs: document optional sentry module"
```

### Task 8: Add CI/Script Coverage For Both Modules

**Files:**
- Modify: existing CI config if present
- Or create: `scripts/test-all.sh`

**Step 1: Check for existing CI**

Run:

```sh
find . -maxdepth 3 -type f | rg 'github/workflows|buildkite|circle|ci|Makefile'
```

If CI exists, update it to test both modules. If not, create `scripts/test-all.sh`.

**Step 2: Add test script if needed**

Create:

```sh
#!/usr/bin/env bash
set -euo pipefail

go test ./...
if go list -deps ./... | grep -E 'github.com/getsentry|sentry-go'; then
  echo "root module unexpectedly depends on sentry-go" >&2
  exit 1
fi

(cd sentry && go test ./...)
(cd sentry && go list -deps ./... | grep 'github.com/getsentry/sentry-go' >/dev/null)
```

Make executable:

```sh
chmod +x scripts/test-all.sh
```

**Step 3: Run script or CI-equivalent commands**

Run:

```sh
./scripts/test-all.sh
```

Expected: PASS.

**Step 4: Commit**

```sh
git add .github scripts
git commit -m "ci: test root and optional sentry modules"
```

If only one of `.github` or `scripts` exists or changed, add only the changed path. If no files changed because CI already covered both modules, skip the commit.

### Task 9: Final Verification

**Files:**
- Verify all changed files

**Step 1: Verify root module**

Run:

```sh
go test ./...
go mod tidy
git diff --exit-code go.mod go.sum
go list -deps ./... | rg "github.com/getsentry|sentry-go"
```

Expected:

- tests pass
- no `go.mod` / `go.sum` diff
- dependency grep returns no matches

**Step 2: Verify optional Sentry module**

Run:

```sh
(cd sentry && go test ./...)
(cd sentry && go mod tidy)
git diff --exit-code sentry/go.mod sentry/go.sum
(cd sentry && go list -deps ./... | rg "github.com/getsentry/sentry-go")
```

Expected:

- tests pass
- no nested module diff
- dependency grep finds `github.com/getsentry/sentry-go`

**Step 3: Verify source imports**

Run:

```sh
rg 'github.com/getsentry|sentry-go|BuildSentryReport|ReportError|ReportableStackTrace|GetReportableStackTrace' --glob '*.go' .
```

Expected:

- Sentry imports and Sentry API names only appear under `sentry/`, except neutral documentation or comments that intentionally mention optional Sentry support.
- No root package file exposes Sentry-native API.

**Step 4: Commit final cleanup**

```sh
git add .
git commit -m "chore: finish optional sentry split"
```

Skip this commit if there are no changes.
