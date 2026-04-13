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
	"fmt"
	"testing"

	"github.com/cockroachdb/errors/errbase"
	"github.com/cockroachdb/errors/errorspb"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/require"
)

type protoRoundTripError struct {
	value string
}

func (e *protoRoundTripError) Error() string {
	return "proto round trip: " + e.value
}

func registerProtoRoundTripError(t *testing.T) {
	t.Helper()

	typeKey := errbase.GetTypeKey((*protoRoundTripError)(nil))
	errbase.RegisterLeafEncoder(typeKey, func(_ context.Context, err error) (string, []string, proto.Message) {
		orig := err.(*protoRoundTripError)
		return orig.Error(), []string{orig.value}, &errorspb.StringsPayload{Details: []string{orig.value}}
	})
	errbase.RegisterLeafDecoder(typeKey, func(_ context.Context, msg string, safeDetails []string, payload proto.Message) error {
		m, ok := payload.(*errorspb.StringsPayload)
		if !ok || len(m.Details) == 0 {
			return nil
		}
		return &protoRoundTripError{value: m.Details[0]}
	})
	t.Cleanup(func() {
		errbase.RegisterLeafEncoder(typeKey, nil)
		errbase.RegisterLeafDecoder(typeKey, nil)
	})
}

func roundTripEncodedErrorProto(t *testing.T, enc errbase.EncodedError) errbase.EncodedError {
	t.Helper()

	raw, err := proto.Marshal(&enc)
	require.NoError(t, err)

	var decoded errorspb.EncodedError
	require.NoError(t, proto.Unmarshal(raw, &decoded))
	return decoded
}

func makeUnknownEncodedError(typeURL, typeName, msg string) errbase.EncodedError {
	return errbase.EncodedError{
		Error: &errorspb.EncodedError_Leaf{
			Leaf: &errorspb.EncodedErrorLeaf{
				Message: msg,
				Details: errorspb.EncodedErrorDetails{
					OriginalTypeName: typeName,
					ErrorTypeMark: errorspb.ErrorTypeMark{
						FamilyName: typeName,
					},
					FullDetails: &types.Any{
						TypeUrl: typeURL,
						Value:   []byte{0xde, 0xad, 0xbe, 0xef},
					},
				},
			},
		},
	}
}

func requireOpaqueErrorType(t *testing.T, err error) {
	t.Helper()
	require.Contains(t, fmt.Sprintf("%T", err), "opaque")
}
