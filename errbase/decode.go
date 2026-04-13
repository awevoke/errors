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

package errbase

import (
	"context"

	"github.com/cockroachdb/errors/errorspb"
	"github.com/cockroachdb/errors/internal/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// DecodeError decodes an error.
//
// Can only be called if the EncodedError is set (see IsSet()).
func DecodeError(ctx context.Context, enc EncodedError) error {
	return DecodeErrorWithOptions(ctx, enc, DecodeOptions{Resolver: protoregistry.GlobalTypes})
}

// DecodeOptions configures DecodeErrorWithOptions.
type DecodeOptions struct {
	Resolver protoregistry.MessageTypeResolver
}

// DecodeErrorWithOptions decodes an error with explicit protobuf options.
//
// Can only be called if the EncodedError is set (see IsSet()).
func DecodeErrorWithOptions(ctx context.Context, enc EncodedError, opts DecodeOptions) error {
	if opts.Resolver == nil {
		opts.Resolver = protoregistry.GlobalTypes
	}
	if w := enc.GetWrapper(); w != nil {
		return decodeWrapper(ctx, w, opts)
	}
	return decodeLeaf(ctx, enc.GetLeaf(), opts)
}

func decodeLeaf(ctx context.Context, enc *errorspb.EncodedErrorLeaf, opts DecodeOptions) error {
	details := enc.GetDetails()

	// In case there is a detailed payload, decode it.
	var payload proto.Message
	if details.GetFullDetails() != nil {
		var err error
		payload, err = protowire.UnmarshalAny(details.GetFullDetails(), opts.Resolver)
		if err != nil {
			// It's OK if we can't decode. We'll use
			// the opaque type below.
			warningFn(ctx, "error while unmarshalling error: %+v", err)
		}
	}

	// Do we have a leaf decoder for this type?
	typeKey := TypeKey(details.GetErrorTypeMark().GetFamilyName())
	if decoder, ok := leafDecoders[typeKey]; ok {
		// Yes, use it.
		genErr := decoder(ctx, enc.GetMessage(), details.GetReportablePayload(), payload)
		if genErr != nil {
			// Decoding succeeded. Use this.
			return genErr
		}
		// Decoding failed, we'll drop through to opaqueLeaf{} below.
	} else if decoder, ok := multiCauseDecoders[typeKey]; ok {
		causes := decodeCauses(ctx, enc.GetMultierrorCauses(), opts)
		genErr := decoder(ctx, causes, enc.GetMessage(), details.GetReportablePayload(), payload)
		if genErr != nil {
			return genErr
		}
	} else {
		// Shortcut for non-registered proto-encodable error types:
		// if it already implements `error`, it's good to go.
		if e, ok := payload.(error); ok {
			// yes: we're done!
			return e
		}
	}

	if len(enc.GetMultierrorCauses()) > 0 {
		leaf := &opaqueLeafCauses{
			causes: decodeCauses(ctx, enc.GetMultierrorCauses(), opts),
		}
		leaf.msg = enc.GetMessage()
		leaf.details = details
		return leaf
	}

	// No decoder and no error type: we'll keep what we received and
	// make it ready to re-encode exactly (if the error leaves over the
	// network again).
	return &opaqueLeaf{
		msg:     enc.GetMessage(),
		details: details,
	}
}

func decodeCauses(ctx context.Context, encs []*errorspb.EncodedError, opts DecodeOptions) []error {
	causes := make([]error, len(encs))
	for i, e := range encs {
		if e != nil {
			causes[i] = DecodeErrorWithOptions(ctx, *e, opts)
		}
	}
	return causes
}

func decodeWrapper(ctx context.Context, enc *errorspb.EncodedWrapper, opts DecodeOptions) error {
	// First decode the cause.
	var cause error
	if enc.GetCause() != nil {
		cause = DecodeErrorWithOptions(ctx, *enc.GetCause(), opts)
	}

	details := enc.GetDetails()

	// In case there is a detailed payload, decode it.
	var payload proto.Message
	if details.GetFullDetails() != nil {
		var err error
		payload, err = protowire.UnmarshalAny(details.GetFullDetails(), opts.Resolver)
		if err != nil {
			// It's OK if we can't decode. We'll use
			// the opaque type below.
			warningFn(ctx, "error while unmarshalling wrapper error: %+v", err)
		}
	}

	// Do we have a wrapper decoder for this?
	typeKey := TypeKey(details.GetErrorTypeMark().GetFamilyName())
	if decoder, ok := decoders[typeKey]; ok {
		// Yes, use it.
		genErr := decoder(ctx, cause, enc.GetMessage(), details.GetReportablePayload(), payload)
		if genErr != nil {
			// Decoding succeeded. Use this.
			return genErr
		}
		// Decoding failed, we'll drop through to opaqueWrapper{} below.
	}

	// Otherwise, preserve all details about the original object.
	return &opaqueWrapper{
		cause:       cause,
		prefix:      enc.GetMessage(),
		details:     details,
		messageType: MessageType(enc.GetMessageType()),
	}
}

// RegisterLeafDecoder can be used to register new leaf error types to
// the library. Registered types will be decoded using their own
// Go type when an error is decoded. Wrappers that have not been
// registered will be decoded using the opaqueLeaf type.
//
// Note: if the error type has been migrated from a previous location
// or a different type, ensure that RegisterTypeMigration() was called
// prior to RegisterLeafDecoder().
func RegisterLeafDecoder(theType TypeKey, decoder LeafDecoder) {
	if decoder == nil {
		delete(leafDecoders, theType)
	} else {
		leafDecoders[theType] = decoder
	}
}

// LeafDecoder is to be provided (via RegisterLeafDecoder above)
// by additional wrapper types not yet known to this library.
// A nil return indicates that decoding was not successful.
type LeafDecoder = func(ctx context.Context, msg string, safeDetails []string, payload proto.Message) error

// registry for RegisterLeafDecoder.
var leafDecoders = map[TypeKey]LeafDecoder{}

// RegisterWrapperDecoder can be used to register new wrapper types to
// the library. Registered wrappers will be decoded using their own
// Go type when an error is decoded. Wrappers that have not been
// registered will be decoded using the opaqueWrapper type.
//
// Note: if the error type has been migrated from a previous location
// or a different type, ensure that RegisterTypeMigration() was called
// prior to RegisterWrapperDecoder().
func RegisterWrapperDecoder(theType TypeKey, decoder WrapperDecoder) {
	if decoder == nil {
		delete(decoders, theType)
	} else {
		decoders[theType] = decoder
	}
}

// WrapperDecoder is to be provided (via RegisterWrapperDecoder above)
// by additional wrapper types not yet known to this library.
// A nil return indicates that decoding was not successful.
type WrapperDecoder = func(ctx context.Context, cause error, msgPrefix string, safeDetails []string, payload proto.Message) error

// registry for RegisterWrapperType.
var decoders = map[TypeKey]WrapperDecoder{}

// MultiCauseDecoder is to be provided (via RegisterMultiCauseDecoder
// above) by additional multi-cause wrapper types not yet known by the
// library. A nil return indicates that decoding was not successful.
type MultiCauseDecoder = func(ctx context.Context, causes []error, msgPrefix string, safeDetails []string, payload proto.Message) error

// registry for RegisterMultiCauseDecoder.
var multiCauseDecoders = map[TypeKey]MultiCauseDecoder{}

// RegisterMultiCauseDecoder can be used to register new multi-cause
// wrapper types to the library. Registered wrappers will be decoded
// using their own Go type when an error is decoded. Multi-cause
// wrappers that have not been registered will be decoded using the
// opaqueWrapper type.
func RegisterMultiCauseDecoder(theType TypeKey, decoder MultiCauseDecoder) {
	if decoder == nil {
		delete(multiCauseDecoders, theType)
	} else {
		multiCauseDecoders[theType] = decoder
	}
}
