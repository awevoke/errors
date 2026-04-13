package protowire

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/anypb"
)

const typeURLPrefix = "type.googleapis.com/"

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

func Marshal(m proto.Message) ([]byte, error) {
	if vt, ok := m.(vtMarshaler); ok {
		return vt.MarshalVT()
	}
	return proto.Marshal(m)
}

func Unmarshal(b []byte, m proto.Message) error {
	if vt, ok := m.(vtUnmarshaler); ok {
		proto.Reset(m)
		return vt.UnmarshalVT(b)
	}
	return proto.Unmarshal(b, m)
}

func Size(m proto.Message) int {
	if vt, ok := m.(vtSizer); ok {
		return vt.SizeVT()
	}
	return proto.Size(m)
}

func Equal(a, b proto.Message) bool {
	if vt, ok := a.(vtEqualer); ok {
		return vt.EqualMessageVT(b)
	}
	return proto.Equal(a, b)
}

func MarshalAny(m proto.Message) (*anypb.Any, error) {
	b, err := Marshal(m)
	if err != nil {
		return nil, err
	}
	name := m.ProtoReflect().Descriptor().FullName()
	return &anypb.Any{
		TypeUrl: typeURLPrefix + string(name),
		Value:   b,
	}, nil
}

func UnmarshalAny(a *anypb.Any, resolver protoregistry.MessageTypeResolver) (proto.Message, error) {
	if resolver == nil {
		resolver = protoregistry.GlobalTypes
	}
	mt, err := resolver.FindMessageByURL(a.GetTypeUrl())
	if err != nil {
		return nil, err
	}
	m := mt.New().Interface()
	if err := Unmarshal(a.GetValue(), m); err != nil {
		return nil, err
	}
	return m, nil
}
