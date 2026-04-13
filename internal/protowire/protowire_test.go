package protowire

import (
	"bytes"
	"testing"

	"github.com/cockroachdb/errors/errorspb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type fakeVTMessage struct {
	errorspb.StringsPayload
	marshalCalls   int
	unmarshalCalls int
	unmarshalReset bool
	sizeCalls      int
	equalCalls     int
}

func (f *fakeVTMessage) MarshalVT() ([]byte, error) {
	f.marshalCalls++
	return []byte("vt-marshal"), nil
}

func (f *fakeVTMessage) UnmarshalVT(b []byte) error {
	f.unmarshalCalls++
	f.unmarshalReset = len(f.Details) == 0
	f.Details = []string{string(b)}
	return nil
}

func (f *fakeVTMessage) SizeVT() int {
	f.sizeCalls++
	return 47
}

func (f *fakeVTMessage) EqualMessageVT(other proto.Message) bool {
	f.equalCalls++
	_, ok := other.(*fakeVTMessage)
	return ok
}

func TestMarshalPrefersVTAndFallsBack(t *testing.T) {
	fake := &fakeVTMessage{}

	got, err := Marshal(fake)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("vt-marshal")) {
		t.Fatalf("Marshal() = %q, want vt bytes", got)
	}
	if fake.marshalCalls != 1 {
		t.Fatalf("MarshalVT calls = %d, want 1", fake.marshalCalls)
	}

	standard := wrapperspb.String("fallback")
	got, err = Marshal(standard)
	if err != nil {
		t.Fatal(err)
	}
	want, err := proto.Marshal(standard)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fallback Marshal() = %v, want %v", got, want)
	}
}

func TestUnmarshalResetsBeforeVTAndFallsBack(t *testing.T) {
	fake := &fakeVTMessage{StringsPayload: errorspb.StringsPayload{Details: []string{"stale"}}}
	if err := Unmarshal([]byte("fresh"), fake); err != nil {
		t.Fatal(err)
	}
	if fake.unmarshalCalls != 1 {
		t.Fatalf("UnmarshalVT calls = %d, want 1", fake.unmarshalCalls)
	}
	if !fake.unmarshalReset {
		t.Fatal("UnmarshalVT saw stale state; proto.Reset was not called first")
	}
	if got := fake.GetDetails(); len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("details = %v, want [fresh]", got)
	}

	src := wrapperspb.String("fallback")
	wire, err := proto.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := wrapperspb.String("stale")
	if err := Unmarshal(wire, dst); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(src, dst) {
		t.Fatalf("fallback Unmarshal() = %v, want %v", dst, src)
	}
}

func TestSizePrefersVTAndGeneratedSizeVTWorks(t *testing.T) {
	fake := &fakeVTMessage{}
	if got := Size(fake); got != 47 {
		t.Fatalf("Size() = %d, want fake VT size", got)
	}
	if fake.sizeCalls != 1 {
		t.Fatalf("SizeVT calls = %d, want 1", fake.sizeCalls)
	}

	generated := &errorspb.StringsPayload{Details: []string{"alpha", "beta"}}
	if got, want := Size(generated), generated.SizeVT(); got != want {
		t.Fatalf("generated Size() = %d, want SizeVT %d", got, want)
	}
}

func TestEqualPrefersVTAndFallsBack(t *testing.T) {
	left := &fakeVTMessage{}
	right := &fakeVTMessage{}
	if !Equal(left, right) {
		t.Fatal("Equal() = false, want true")
	}
	if left.equalCalls != 1 {
		t.Fatalf("EqualMessageVT calls = %d, want 1", left.equalCalls)
	}

	if !Equal(wrapperspb.String("same"), wrapperspb.String("same")) {
		t.Fatal("fallback Equal() = false, want true")
	}
	if Equal(wrapperspb.String("left"), wrapperspb.String("right")) {
		t.Fatal("fallback Equal() = true, want false")
	}
}

func TestMarshalAnyAndUnmarshalAny(t *testing.T) {
	src := &errorspb.StringsPayload{Details: []string{"detail"}}
	packed, err := MarshalAny(src)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := packed.TypeUrl, "type.googleapis.com/cockroach.errorspb.StringsPayload"; got != want {
		t.Fatalf("TypeUrl = %q, want %q", got, want)
	}
	if len(packed.Value) != Size(src) {
		t.Fatalf("Any value length = %d, want Size(src) %d", len(packed.Value), Size(src))
	}

	msg, err := UnmarshalAny(packed, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := msg.(*errorspb.StringsPayload)
	if !ok {
		t.Fatalf("UnmarshalAny() type = %T, want *errorspb.StringsPayload", msg)
	}
	if !Equal(src, got) {
		t.Fatalf("UnmarshalAny() = %v, want %v", got, src)
	}

	resolver := new(protoregistry.Types)
	if err := resolver.RegisterMessage(src.ProtoReflect().Type()); err != nil {
		t.Fatal(err)
	}
	msg, err = UnmarshalAny(packed, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !Equal(src, msg) {
		t.Fatalf("UnmarshalAny() with custom resolver = %v, want %v", msg, src)
	}
}

func TestUnmarshalAnyUnknownType(t *testing.T) {
	_, err := UnmarshalAny(&anypb.Any{TypeUrl: "type.googleapis.com/cockroach.errorspb.DoesNotExist"}, nil)
	if err == nil {
		t.Fatal("UnmarshalAny() error = nil, want unknown type error")
	}
}
