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
	"github.com/cockroachdb/errors/testutils"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
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
	require.NotNil(t, leaf.Details.FullDetails)

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
	require.NotNil(t, wrapper.Details.FullDetails)

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
