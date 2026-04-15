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

package errors

import "github.com/cockroachdb/errors/withstack"

// This file mirrors the WithStack functionality from
// github.com/pkg/errors. We would prefer to reuse the withStack
// struct from that package directly (the library recognizes it well)
// unfortunately github.com/pkg/errors does not enable client code to
// customize the depth at which the stack trace is captured.

// WithStack annotates err with a stack trace at the point WithStack was called.
//
// Detail is shown:
// - via `errors.GetSafeDetails()`
// - when formatting with `%+v`.
// - in Sentry reports.
// - when innermost stack capture, with `errors.GetOneLineSource()`.
func WithStack(err error) error { return withstack.WithStackDepth(err, 1) }

// WithStackDepth annotates err with a stack trace starting from the
// given call depth. The value zero identifies the caller
// of WithStackDepth itself.
// See the documentation of WithStack() for more details.
func WithStackDepth(err error, depth int) error { return withstack.WithStackDepth(err, depth+1) }

// GetOneLineSource extracts the file/line/function information
// of the topmost caller in the innermost recorded stack trace.
// The filename is simplified to remove the path prefix.
//
// This is used e.g. to populate the "source" field in
// PostgreSQL errors in CockroachDB.
func GetOneLineSource(err error) (file string, line int, fn string, ok bool) {
	return withstack.GetOneLineSource(err)
}
