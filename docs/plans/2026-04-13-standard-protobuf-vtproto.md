# Standard Protobuf and vtprotobuf Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace all gogo protobuf/status usage with standard protobuf APIs and vtprotobuf fast paths.

**Architecture:** Standard `google.golang.org/protobuf` becomes the only public protobuf runtime. vtprotobuf is generated for repo-owned messages and used through private structural type assertions in the repo-wide internal helper package `github.com/cockroachdb/errors/internal/protowire`, falling back to standard protobuf for external messages. Standard `anypb.Any` and `protoregistry` replace gogo `types.Any` and `DynamicAny`.

**Tech Stack:** Go, `google.golang.org/protobuf`, `google.golang.org/grpc`, `github.com/planetscale/vtprotobuf`, `protoc-gen-go`, `protoc-gen-go-grpc`, `protoc-gen-go-vtproto`.

---

### Task 1: Add Migration Guard Tests

**Files:**
- Modify: `errbase/encode_test.go`
- Modify: `extgrpc/ext_grpc_test.go`
- Create: `errbase/proto_helpers_test.go`

**Step 1: Add current behavior coverage before changing runtime**

Add tests that assert:

- `errbase.EncodeError` output can be marshaled and unmarshaled through protobuf.
- An encoded payload in `FullDetails` can be decoded back through the current registry path.
- Unknown payload decode still produces an opaque error and can be re-encoded.
- Current gRPC status behavior is captured, including the existing standard/gogo detail split. This test should be updated later to assert standard details are preserved after the runtime migration.

Use current APIs for this task. These tests are allowed to import gogo before the migration.

**Step 2: Run focused tests**

Run:

```sh
go test ./errbase ./extgrpc
```

Expected: PASS.

**Step 3: Commit**

```sh
git add errbase/encode_test.go extgrpc/ext_grpc_test.go errbase/proto_helpers_test.go
git commit -m "test: cover protobuf error round trips"
```

### Task 2: Update Proto Schema and Generation

**Files:**
- Modify: `Makefile.update-protos`
- Modify: `errbase/internal/unknown.proto`
- Modify: `errorspb/*.proto`
- Modify: `extgrpc/ext_grpc.proto`
- Modify: `exthttp/ext_http.proto`
- Modify: `grpc/echoer.proto`
- Modify: `markers/internal/unknown.proto`
- Modify: `go.mod`

**Step 1: Update `go_package` options**

Set full import paths with package aliases, for example:

```proto
option go_package = "github.com/cockroachdb/errors/errorspb;errorspb";
```

Use the correct package path for every `.proto`.

**Step 2: Remove gogo proto options**

Remove:

```proto
import "gogoproto/gogo.proto";
[(gogoproto.nullable) = false]
```

Do not replace these with custom options.

**Step 3: Replace generator makefile**

Use standard and vtproto generators for message generation:

```make
protoc \
  -I. \
  --go_out=paths=source_relative:. \
  --go-vtproto_out=paths=source_relative:. \
  --go-vtproto_opt=features=marshal+unmarshal+size+equal+clone \
  $$dir/*.proto
```

Run `protoc-gen-go-grpc` only for service-bearing protos. In the current repo that is `grpc/echoer.proto`; `extgrpc/ext_grpc.proto` is message-only and must not produce a dead `ext_grpc_grpc.pb.go`.

```make
protoc \
  -I. \
  --go-grpc_out=paths=source_relative:. \
  grpc/echoer.proto
```

**Step 4: Add tool dependencies**

Add `github.com/planetscale/vtprotobuf v0.6.0` to `go.mod`. Keep standard protobuf and grpc dependencies.

Plan to remove these modules after source imports are gone:

- `github.com/gogo/protobuf`
- `github.com/gogo/status`
- `github.com/gogo/googleapis`

Run `go mod tidy` after the source migration so `go.sum` drops unused gogo entries.

**Step 5: Regenerate protos**

Run:

```sh
make -f Makefile.update-protos
```

Expected: old gogo-generated `.pb.go` files are replaced by standard `.pb.go`; `_vtproto.pb.go` files are added.

**Step 6: Commit**

```sh
git add -A Makefile.update-protos go.mod go.sum errbase/internal errorspb extgrpc exthttp grpc markers/internal
git commit -m "build: regenerate protos with standard protobuf and vtproto"
```

### Task 3: Add Internal Protobuf Wire Helpers

**Files:**
- Create: `internal/protowire/protowire.go`
- Create: `internal/protowire/protowire_test.go`

**Step 1: Implement private vt method assertions**

Define only unexported structural interfaces for vt methods. Do not create public duplicate protobuf interfaces.

Required helper functions:

```go
func Marshal(m proto.Message) ([]byte, error)
func Unmarshal(b []byte, m proto.Message) error
func Size(m proto.Message) int
func Equal(a, b proto.Message) bool
func MarshalAny(m proto.Message) (*anypb.Any, error)
func UnmarshalAny(a *anypb.Any, resolver protoregistry.MessageTypeResolver) (proto.Message, error)
```

Implementation rules:

- Prefer `MarshalVT`, `UnmarshalVT`, `SizeVT`, and `EqualMessageVT` when present.
- Call `proto.Reset(m)` before `UnmarshalVT`.
- Fall back to standard protobuf APIs.
- If resolver is nil, use `protoregistry.GlobalTypes`.
- Use `FindMessageByURL` for `Any` decode.
- Do not add deterministic marshal, clone, or pool helpers unless the migration adds a concrete call site for them. `clone` is generated for messages but does not need a wrapper unless code needs generic clone dispatch.

**Step 2: Test helper dispatch**

Use small local fake types where possible for interface dispatch, and generated repo protos for actual marshal/unmarshal behavior. Cover `Size` so `SizeVT` generation is exercised through a real helper.

Run:

```sh
go test ./internal/protowire
```

Expected: PASS.

**Step 3: Commit**

```sh
git add internal/protowire
git commit -m "internal: add vt-aware protobuf helpers"
```

### Task 4: Port errbase Encoding and Decoding

**Files:**
- Modify: `errbase/encode.go`
- Modify: `errbase/decode.go`
- Modify: `errbase/adapters.go`
- Modify: `errbase/adapters_errno.go`
- Modify: `errbase/safe_details.go`
- Modify: `errbase/opaque.go`
- Modify: `errbase/*_test.go`

**Step 1: Replace imports**

Replace gogo imports with:

```go
import "google.golang.org/protobuf/proto"
```

Use `google.golang.org/protobuf/types/known/anypb` only in helper code or where direct type references are necessary.

**Step 2: Update `Any` handling**

Replace `types.MarshalAny` and `types.DynamicAny` with `github.com/cockroachdb/errors/internal/protowire.MarshalAny` and `github.com/cockroachdb/errors/internal/protowire.UnmarshalAny`.

**Step 3: Handle pointer field shape changes**

Initialize nested protobuf fields explicitly during encode. Use generated getters or local nil-safe helpers during decode, formatting, and reporting.

**Step 4: Add decode options**

Add:

```go
type DecodeOptions struct {
	Resolver protoregistry.MessageTypeResolver
}

func DecodeErrorWithOptions(ctx context.Context, enc EncodedError, opts DecodeOptions) error
```

Keep `DecodeError(ctx, enc)` as a wrapper using default options and `protoregistry.GlobalTypes`. `DecodeOptions` is intentional public API surface for decode behavior; do not replace it with a one-off resolver function.

**Step 5: Run tests**

```sh
go test ./errbase
```

Expected: PASS.

**Step 6: Commit**

```sh
git add errbase
git commit -m "errbase: use standard protobuf runtime"
```

### Task 5: Port Error Packages to Standard protobuf

**Files:**
- Modify: `assert/assert.go`
- Modify: `barriers/barriers.go`
- Modify: `contexttags/with_context.go`
- Modify: `domains/with_domain.go`
- Modify: `errutil/redactable.go`
- Modify: `exthttp/ext_http.go`
- Modify: `hintdetail/with_detail.go`
- Modify: `hintdetail/with_hint.go`
- Modify: `issuelink/unimplemented_error.go`
- Modify: `issuelink/with_issuelink.go`
- Modify: `join/join.go`
- Modify: `markers/markers.go`
- Modify: `safedetails/with_safedetails.go`
- Modify: `secondary/with_secondary.go`
- Modify: `telemetrykeys/with_telemetry.go`
- Modify: related tests

**Step 1: Replace gogo proto imports**

Use:

```go
import "google.golang.org/protobuf/proto"
```

**Step 2: Update pointer-shaped payloads**

For repeated message fields generated as pointers, append pointer values:

```go
p.Tags = append(p.Tags, &errorspb.TagPayload{Tag: key, Value: value})
```

For decoded payloads, use getters or nil checks.

**Step 3: Run package tests**

```sh
go test ./assert ./barriers ./contexttags ./domains ./errutil ./exthttp ./hintdetail ./issuelink ./join ./markers ./safedetails ./secondary ./telemetrykeys
```

Expected: PASS.

**Step 4: Commit**

```sh
git add assert barriers contexttags domains errutil exthttp hintdetail issuelink join markers safedetails secondary telemetrykeys
git commit -m "errors: port payload encoders to standard protobuf"
```

### Task 6: Port gRPC Support

**Files:**
- Modify: `extgrpc/ext_grpc.go`
- Modify: `extgrpc/ext_grpc_test.go`
- Modify: `grpc/middleware/server.go`
- Modify: `grpc/middleware/client.go`
- Modify: `grpc/status/status.go`
- Modify: `grpc/*.go`

**Step 1: Remove gogo status imports**

Use `google.golang.org/grpc/status` throughout.

**Step 2: Encode standard status payloads**

Use `status.Convert(err)` and `Status.Proto()` for status payloads. Payload type should be `*google.golang.org/genproto/googleapis/rpc/status.Status`, which is returned by `status.Status.Proto()`.

**Step 3: Decode standard status payloads**

Assert the payload to the standard status protobuf type and return `status.ErrorProto(payload)`.

**Step 3a: Use vt-aware equality in protobuf comparisons**

Where tests compare protobuf messages, replace direct `proto.Equal` calls with `github.com/cockroachdb/errors/internal/protowire.Equal` so the `EqualMessageVT` path has a real consumer.

**Step 4: Update middleware**

Server and client interceptors should use standard `status` and standard details. Remove comments about gogoproto registration.

**Step 5: Decide vt gRPC codec exposure**

Do not globally register vtprotobuf's codec in `init`. If needed, add a small documented opt-in helper or example using `github.com/planetscale/vtprotobuf/codec/grpc`.

**Step 6: Run tests**

```sh
go test ./extgrpc ./grpc/...
```

Expected: PASS.

**Step 7: Commit**

```sh
git add extgrpc grpc
git commit -m "grpc: use standard protobuf status details"
```

### Task 7: Port Remaining Tests and Docs

**Files:**
- Modify: `README.md`
- Modify: `fmttests/format_error_test.go`
- Modify: `fmttests/testdata/format/*`
- Modify: `errbase/migrations_test.go`
- Modify: `errbase/unknown_type_test.go`
- Modify: any files still importing gogo

**Step 1: Replace README examples**

Replace gogoproto imports and prose with standard protobuf imports:

```go
import "google.golang.org/protobuf/proto"
```

Remove claims about gogo status support.

**Step 2: Replace test imports**

Use standard protobuf APIs and `github.com/cockroachdb/errors/internal/protowire` helpers where tests need vt-aware marshal or equality behavior.

**Step 2a: Refresh formatting fixtures**

Update `fmttests/testdata/format/*` expected-output fixtures after generated protobuf shapes change. The fixtures currently include gogo-shaped values such as `(*types.Any)(nil)` and must reflect standard `anypb.Any` and pointer-shaped nested messages.

**Step 2b: Remove gogo modules**

After all source imports are gone, run:

```sh
go mod tidy
```

Confirm `go.mod` no longer requires:

- `github.com/gogo/protobuf`
- `github.com/gogo/status`
- `github.com/gogo/googleapis`

**Step 3: Search for gogo references**

Run:

```sh
rg "github.com/gogo|gogoproto|protoc-gen-gogo|gogoroach" .
```

Expected: only design/plan references remain. If the project wants zero textual references, remove them from docs too.

**Step 4: Run all tests**

```sh
go test ./...
```

Expected: PASS.

**Step 5: Commit**

```sh
git add README.md fmttests errbase go.mod go.sum internal
git commit -m "docs: update protobuf migration references"
```

### Task 8: Final Verification

**Files:**
- Verify all changed files

**Step 1: Run full test suite**

```sh
go test ./...
```

Expected: PASS.

**Step 2: Verify no gogo code references**

```sh
rg "github.com/gogo|gogoproto|protoc-gen-gogo|gogoroach" --glob '!docs/plans/*' .
go list -m all | rg "github.com/gogo"
```

Expected: no matches from either command.

**Step 3: Verify generated code is current**

```sh
make -f Makefile.update-protos
git diff --exit-code
```

Expected: no diff.

**Step 4: Commit any final cleanup**

```sh
git add .
git commit -m "chore: finish standard protobuf migration"
```

Skip this commit if there are no changes.
