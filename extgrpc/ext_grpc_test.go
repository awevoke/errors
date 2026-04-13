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

package extgrpc_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
	"github.com/cockroachdb/errors/errorspb"
	"github.com/cockroachdb/errors/extgrpc"
	"github.com/cockroachdb/errors/internal/protowire"
	"github.com/cockroachdb/errors/testutils"
	"github.com/stretchr/testify/require"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/protoadapt"
)

func TestGrpc(t *testing.T) {
	err := fmt.Errorf("hello")
	err = extgrpc.WrapWithGrpcCode(err, codes.Unavailable)

	// Simulate a network transfer.
	enc := errors.EncodeError(context.Background(), err)
	otherErr := errors.DecodeError(context.Background(), enc)

	tt := testutils.T{T: t}

	// Error is preserved through the network.
	tt.CheckDeepEqual(otherErr, err)

	// It's possible to extract the Grpc code.
	tt.CheckEqual(extgrpc.GetGrpcCode(otherErr), codes.Unavailable)

	// If there are multiple codes, the most recent one wins.
	otherErr = extgrpc.WrapWithGrpcCode(otherErr, codes.NotFound)
	tt.CheckEqual(extgrpc.GetGrpcCode(otherErr), codes.NotFound)

	// The code is hidden when the error is printed with %v.
	tt.CheckStringEqual(fmt.Sprintf("%v", err), `hello`)
	// The code appears when the error is printed verbosely.
	tt.CheckStringEqual(fmt.Sprintf("%+v", err), `hello
(1) gRPC code: Unavailable
Wraps: (2) hello
Error types: (1) *extgrpc.withGrpcCode (2) *errors.errorString`)

	// Checking the code of a nil error should be codes.OK
	var noErr error
	tt.Assert(extgrpc.GetGrpcCode(noErr) == codes.OK)
}

func TestEncodeDecodeStatus(t *testing.T) {
	ctx := context.Background()

	expectedDetails := []proto.Message{
		grpcstatus.New(codes.Internal, "status").Proto(),
		&errorspb.StringsPayload{Details: []string{"foo", "bar"}},
	}
	status := grpcstatus.New(codes.NotFound, "message")
	for _, detail := range expectedDetails {
		var err error
		status, err = status.WithDetails(protoadapt.MessageV1Of(detail))
		require.NoError(t, err)
	}
	require.Equal(t, codes.NotFound, status.Code())
	require.Equal(t, "message", status.Message())

	statusDetails := status.Details()
	require.Equal(t, len(expectedDetails), len(statusDetails), "detail mismatch")
	for i, expectDetail := range expectedDetails {
		requireProtoDetailEqual(t, expectDetail, statusDetails[i], "detail %v", i)
	}

	// Encode the error and check some fields.
	encodedError := errbase.EncodeError(ctx, status.Err())
	leaf := encodedError.GetLeaf()
	require.NotNil(t, leaf, "expected leaf")
	require.Equal(t, status.Message(), leaf.Message)
	require.Empty(t, leaf.GetDetails().GetReportablePayload())
	require.NotNil(t, leaf.GetDetails().GetFullDetails(), "expected full details")
	require.Nil(t, encodedError.GetWrapper(), "unexpected wrapper")

	payload, err := protowire.UnmarshalAny(leaf.GetDetails().GetFullDetails(), nil)
	require.NoError(t, err)
	require.IsType(t, &statuspb.Status{}, payload)

	// Marshal and unmarshal the error, checking that it equals the encoded error.
	marshaledError, err := protowire.Marshal(&encodedError)
	require.NoError(t, err)
	require.NotEmpty(t, marshaledError)

	unmarshaledError := errorspb.EncodedError{}
	err = protowire.Unmarshal(marshaledError, &unmarshaledError)
	require.NoError(t, err)
	require.True(t, protowire.Equal(&encodedError, &unmarshaledError),
		"unmarshaled Protobuf differs")

	// Decode the error.
	decodedError := errbase.DecodeError(ctx, unmarshaledError)
	require.Equal(t, status.Err().Error(), decodedError.Error())

	// Convert the error into a status, and check its properties.
	decodedStatus := grpcstatus.Convert(decodedError)
	require.Equal(t, status.Code(), decodedStatus.Code())
	require.Equal(t, status.Message(), decodedStatus.Message())

	decodedDetails := decodedStatus.Details()
	require.Equal(t, len(expectedDetails), len(decodedDetails), "detail mismatch")
	for i, expectDetail := range expectedDetails {
		requireProtoDetailEqual(t, expectDetail, decodedDetails[i], "detail %v", i)
	}
}

func requireProtoDetailEqual(t *testing.T, expected proto.Message, actual interface{}, msg string, args ...interface{}) {
	t.Helper()
	actualMessage, ok := actual.(proto.Message)
	require.Truef(t, ok, "expected protobuf detail, got %T", actual)
	require.Truef(t, protowire.Equal(expected, actualMessage), msg, args...)
}
