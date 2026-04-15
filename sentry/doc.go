// Package sentry provides optional Sentry reporting support for
// github.com/cockroachdb/errors.
//
// This package intentionally lives in a separate module so default users of
// github.com/cockroachdb/errors do not import or require sentry-go.
package sentry
