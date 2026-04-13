# Standard Protobuf and vtprotobuf Migration Design

## Goal

Replace all `github.com/gogo/*` protobuf and status dependencies with standard `google.golang.org/protobuf` APIs, while generating and using vtprotobuf fast paths for repo-owned messages wherever possible.

## Non-Goals

- Do not preserve gogo protobuf compatibility.
- Do not add a pluggable protobuf abstraction.
- Do not recreate gogo-specific generated shapes such as `nullable=false` value fields.
- Do not introduce public duplicate protobuf or vtprotobuf interfaces when existing public APIs are sufficient.

## Dependencies and Generation

Use standard protobuf generation as the source of truth:

- `google.golang.org/protobuf/cmd/protoc-gen-go`
- `google.golang.org/grpc/cmd/protoc-gen-go-grpc`
- `github.com/planetscale/vtprotobuf/cmd/protoc-gen-go-vtproto`

Run vtprotobuf as an auxiliary generator alongside `protoc-gen-go`, not as a replacement. Generate:

- `marshal`
- `unmarshal`
- `size`
- `equal`
- `clone`

Do not initially generate:

- `marshal_strict`, because no current call site needs deterministic vtprotobuf bytes.
- `unmarshal_unsafe`, because error payloads cross process and network boundaries and should not depend on input buffer lifetime.
- `pool`, because pool ownership is easy to misuse and should be justified by benchmarks after the safe migration is complete.

Update all `.proto` files to use full modern `go_package` values, remove `gogoproto/gogo.proto` imports, and remove all `[(gogoproto.nullable) = false]` options.

## API and Type Strategy

Use standard protobuf types directly in public APIs:

```go
import "google.golang.org/protobuf/proto"

type LeafEncoder func(ctx context.Context, err error) (msg string, safeDetails []string, payload proto.Message)
type LeafDecoder func(ctx context.Context, msg string, safeDetails []string, payload proto.Message) error
type WrapperEncoder func(ctx context.Context, err error) (msgPrefix string, safeDetails []string, payload proto.Message)
type WrapperDecoder func(ctx context.Context, cause error, msgPrefix string, safeDetails []string, payload proto.Message) error
type MultiCauseEncoder func(ctx context.Context, err error) (msg string, safeDetails []string, payload proto.Message)
type MultiCauseDecoder func(ctx context.Context, causes []error, msgPrefix string, safeDetails []string, payload proto.Message) error
```

Do not add a public `Payload`, `Codec`, or protobuf facade type. Standard `proto.Message`, `anypb.Any`, and `protoregistry` are the API.

## vtprotobuf Reuse Policy

Prefer existing exported packages where they exist:

- Use `github.com/planetscale/vtprotobuf/codec/grpc` for opt-in gRPC codec support.
- Use standard protobuf public APIs for the base fallback path.

vtprotobuf does not currently export reusable general-purpose interfaces for generated methods such as `MarshalVT`, `UnmarshalVT`, `SizeVT`, or `EqualMessageVT`; its own codecs define unexported structural interfaces internally. Therefore, if generic helper functions are needed, define only private structural interfaces inside the internal helper package:

```go
type vtMarshaler interface {
	MarshalVT() ([]byte, error)
}

type vtUnmarshaler interface {
	UnmarshalVT([]byte) error
}

type vtSizer interface {
	SizeVT() int
}

type vtEqualer interface {
	EqualMessageVT(proto.Message) bool
}
```

Keep these unexported. They are not new library concepts; they are compile-time structural checks for generated methods.

## Protobuf Operation Helpers

Centralize protobuf wire operations in one small repo-wide internal helper package, `github.com/cockroachdb/errors/internal/protowire` at path `internal/protowire`. This avoids repeated type assertions while keeping the public API standard, and it remains importable from all packages in this module.

Required behavior:

- `Marshal`: prefer `MarshalVT`, fall back to `proto.Marshal`.
- `Unmarshal`: call `proto.Reset` before `UnmarshalVT`, because vtprotobuf documents `UnmarshalVT` as merge-like when the receiver is not zeroed; fall back to `proto.Unmarshal`.
- `Size`: prefer `SizeVT`, fall back to `proto.Size`.
- `Equal`: prefer `EqualMessageVT`, fall back to `proto.Equal`.

These helpers should be private or internal. They should not be exposed as an alternate protobuf API.

## Any and Registry

Use standard `google.golang.org/protobuf/types/known/anypb.Any`.

For packing, avoid `anypb.New` in hot paths because it always uses standard marshal behavior. Build the `Any` with the standard type URL and vt-aware marshal bytes:

```go
func marshalAny(m proto.Message) (*anypb.Any, error) {
	b, err := protowire.Marshal(m)
	if err != nil {
		return nil, err
	}
	name := m.ProtoReflect().Descriptor().FullName()
	return &anypb.Any{
		TypeUrl: "type.googleapis.com/" + string(name),
		Value:   b,
	}, nil
}
```

For unpacking, use the standard registry interfaces:

- `protoregistry.MessageTypeResolver`
- `protoregistry.GlobalTypes`
- `FindMessageByURL`

After resolving the type and allocating a new message, use the vt-aware unmarshal helper. This preserves native type-registry behavior while avoiding reflection on generated messages that have vtprotobuf methods.

Add an optional decode path with a public options struct. The first option is the standard resolver, but the struct is intentional public API surface for decode behavior and future extension:

```go
type DecodeOptions struct {
	Resolver protoregistry.MessageTypeResolver
}

func DecodeErrorWithOptions(ctx context.Context, enc EncodedError, opts DecodeOptions) error
```

Keep `DecodeError(ctx, enc)` as the default path using `protoregistry.GlobalTypes`.

## Generated Struct Shape Changes

Removing `nullable=false` changes generated Go field shapes. Handle this idiomatically with nil checks and constructors, not by adding replacement gogo annotations.

Expected changes include:

- `EncodedErrorLeaf.Details` becomes `*EncodedErrorDetails`.
- `EncodedWrapper.Cause` becomes `*EncodedError`.
- `EncodedWrapper.Details` becomes `*EncodedErrorDetails`.
- `EncodedErrorDetails.ErrorTypeMark` becomes `*ErrorTypeMark`.
- `MarkPayload.Types` becomes `[]*ErrorTypeMark`.
- `TagsPayload.Tags` becomes `[]*TagPayload`.

Encoding paths should initialize required nested messages explicitly. Decode, formatting, and reporting paths should use generated getters or local helper functions to handle nil fields defensively.

## gRPC

Remove `github.com/gogo/status` and `github.com/gogo/googleapis/google/rpc`.

Use:

- `google.golang.org/grpc/status`
- `google.golang.org/grpc/codes`
- standard protobuf status messages returned by `status.Status.Proto()`

The old mixed standard/gogo status-detail limitation should disappear. Standard gRPC details should round-trip as standard protobuf messages.

Do not globally register vtprotobuf's gRPC codec from package `init`. Provide an opt-in helper or documentation for callers that want vtprotobuf serialization in their gRPC stack.

## Documentation

Update README examples and API references to use `google.golang.org/protobuf/proto`. Remove references to gogoproto and gogo status support.

Document that gogo-generated payload messages are no longer accepted as structured payloads. They can still be represented as opaque errors if no structured standard protobuf payload is available.

## Verification

Required checks:

```sh
go test ./...
rg "github.com/gogo|gogoproto|protoc-gen-gogo|gogoroach" .
```

The second command must return no code, proto, README, go.mod, or generated-code references except possibly historical notes in this design document or implementation plan.

Add or update tests covering:

- Standard protobuf marshal/unmarshal round trips for `EncodedError`.
- vt-aware marshal/unmarshal helper behavior.
- `Any` packing and unpacking through `protoregistry.GlobalTypes`.
- Unknown `Any` payload fallback to opaque errors.
- Unknown type traversal after re-encoding.
- Standard gRPC status and details round trips.
- Absence of gogo imports.

## Risks

The largest risk is broad pointer-field churn caused by removing `nullable=false`. This is mechanical but touches many paths.

The second risk is standard `Any` registration. Payload decode only succeeds when the message type is linked into the binary or present in the configured resolver. Existing opaque fallback behavior should remain.

The third risk is deterministic byte behavior. If a future call site needs deterministic bytes, add vtprotobuf `marshal_strict` generation and use `MarshalVTStrict` or standard deterministic marshal there. Do not add deterministic helpers until there is a concrete caller, and do not promise cross-language canonical bytes.
