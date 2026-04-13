// Copyright 2019 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package errbase_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/cockroachdb/errors/errbase"
	"github.com/cockroachdb/errors/errorspb"
	"github.com/cockroachdb/errors/internal/protowire"
	"github.com/cockroachdb/errors/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type myE struct{ marker string }

func (e *myE) Error() string { return "woo" }

func (e *myE) ErrorKeyMarker() string { return e.marker }

var _ errbase.TypeKeyMarker = (*myE)(nil)

// This test shows how the extended type marker changes the visible
// type and thus the identity of an error.
func TestTypeName(t *testing.T) {
	err1 := &myE{"woo"}
	err2 := &myE{""}

	tn1 := errbase.GetTypeMark(err1)
	tn2 := errbase.GetTypeMark(err2)

	tt := testutils.T{T: t}

	tt.Check(!tn1.Equals(tn2))
	tt.CheckEqual(tn1.FamilyName, tn2.FamilyName)
	tt.Check(tn1.Extension != tn2.Extension)
}

func TestEncodeErrorProtoRoundTrip(t *testing.T) {
	registerProtoRoundTripError(t)

	origErr := &protoRoundTripError{value: "payload"}
	enc := errbase.EncodeError(context.Background(), origErr)

	leaf := enc.GetLeaf()
	require.NotNil(t, leaf)
	require.NotNil(t, leaf.GetDetails().GetFullDetails())

	decoded := roundTripEncodedErrorProto(t, enc)
	require.True(t, proto.Equal(&enc, &decoded))

	newErr := errbase.DecodeError(context.Background(), decoded)
	require.Equal(t, origErr, newErr)
}

func TestDecodeUnknownFullDetailsReencodesOpaque(t *testing.T) {
	enc := makeUnknownEncodedError(
		"type.googleapis.com/errbase_test.DoesNotExist",
		"errbase_test.DoesNotExist",
		"missing payload",
	)

	decoded := errbase.DecodeError(context.Background(), enc)
	requireOpaqueErrorType(t, decoded)
	require.Equal(t, "missing payload", decoded.Error())

	reencoded := errbase.EncodeError(context.Background(), decoded)
	require.True(t, proto.Equal(&enc, &reencoded))

	decodedAgain := errbase.DecodeError(context.Background(), reencoded)
	requireOpaqueErrorType(t, decodedAgain)
	require.Equal(t, decoded.Error(), decodedAgain.Error())
}

func TestFullDetailsCurrentRegistryPath(t *testing.T) {
	err := &os.PathError{Op: "open", Path: "/tmp/guard", Err: errors.New("boom")}
	enc := errbase.EncodeError(context.Background(), err)

	wrapper := enc.GetWrapper()
	require.NotNil(t, wrapper)
	require.NotNil(t, wrapper.GetDetails().GetFullDetails())

	decoded := roundTripEncodedErrorProto(t, enc)
	require.True(t, proto.Equal(&enc, &decoded))

	newErr := errbase.DecodeError(context.Background(), decoded)
	require.Equal(t, err.Error(), newErr.Error())

	var decodedPath *os.PathError
	require.ErrorAs(t, newErr, &decodedPath)
	require.Equal(t, err.Op, decodedPath.Op)
	require.Equal(t, err.Path, decodedPath.Path)
	require.Equal(t, err.Err.Error(), decodedPath.Err.Error())
}

type singleMessageResolver struct {
	url string
	mt  protoreflect.MessageType
}

func (r singleMessageResolver) FindMessageByName(protoreflect.FullName) (protoreflect.MessageType, error) {
	return nil, protoregistry.NotFound
}

func (r singleMessageResolver) FindMessageByURL(url string) (protoreflect.MessageType, error) {
	if url == r.url {
		return r.mt, nil
	}
	return nil, protoregistry.NotFound
}

func TestDecodeErrorWithOptionsUsesResolver(t *testing.T) {
	typeKey := errbase.GetTypeKey((*protoRoundTripError)(nil))
	errbase.RegisterLeafDecoder(typeKey, func(_ context.Context, _ string, _ []string, payload proto.Message) error {
		m, ok := payload.(*errorspb.StringsPayload)
		if !ok || len(m.Details) == 0 {
			return nil
		}
		return &protoRoundTripError{value: m.Details[0]}
	})
	t.Cleanup(func() {
		errbase.RegisterLeafDecoder(typeKey, nil)
	})

	payload := &errorspb.StringsPayload{Details: []string{"custom"}}
	fullDetails, err := protowire.MarshalAny(payload)
	require.NoError(t, err)
	fullDetails.TypeUrl = "type.googleapis.com/errbase_test.AliasStringsPayload"

	enc := makeUnknownEncodedError(
		fullDetails.TypeUrl,
		string(typeKey),
		"proto round trip: custom",
	)
	enc.GetLeaf().GetDetails().FullDetails = fullDetails

	decoded := errbase.DecodeErrorWithOptions(context.Background(), enc, errbase.DecodeOptions{
		Resolver: singleMessageResolver{
			url: fullDetails.TypeUrl,
			mt:  payload.ProtoReflect().Type(),
		},
	})
	require.Equal(t, &protoRoundTripError{value: "custom"}, decoded)
}
