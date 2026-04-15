// Copyright 2026 The Cockroach Authors.
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

package fmttests

import (
	"bytes"
	"context"
	goErr "errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/datadriven"
	"github.com/cockroachdb/errors/contexttags"
	"github.com/cockroachdb/errors/domains"
	"github.com/cockroachdb/errors/errbase"
	"github.com/cockroachdb/errors/errutil"
	"github.com/cockroachdb/errors/hintdetail"
	"github.com/cockroachdb/errors/safedetails"
	errorssentry "github.com/cockroachdb/errors/sentry"
	"github.com/cockroachdb/errors/telemetrykeys"
	"github.com/cockroachdb/errors/withstack"
	"github.com/cockroachdb/logtags"
	"github.com/cockroachdb/redact"
	sentrygo "github.com/getsentry/sentry-go"
)

const testPath = "testdata/report"

type arg = datadriven.CmdArg

type commandFn func(inputErr error, args []arg) error

var leafCommands = map[string]commandFn{
	"goerr": func(_ error, args []arg) error { return goErr.New(strfy(args)) },
	"newf": func(_ error, args []arg) error {
		return errutil.Newf("new-style %s", strfy(args))
	},
	"safe-leaf": func(_ error, args []arg) error {
		return errutil.Newf("leaf safe %s", redact.Safe(strfy(args)))
	},
	"assertion": func(_ error, args []arg) error {
		return errutil.AssertionFailedf("assertmsg %s", redact.Safe(strfy(args)))
	},
}

var wrapCommands = map[string]commandFn{
	"wrapf": func(err error, args []arg) error {
		return errutil.Wrapf(err, "wrapped %s", strfy(args))
	},
	"stack": func(err error, _ []arg) error {
		return withstack.WithStack(err)
	},
	"domain": func(err error, args []arg) error {
		return domains.WithDomain(err, domains.NamedDomain(strfy(args)))
	},
	"tags": func(err error, _ []arg) error {
		ctx := context.Background()
		ctx = logtags.AddTag(ctx, "k", 123)
		ctx = logtags.AddTag(ctx, "safe", redact.Safe(456))
		return contexttags.WithContextTags(err, ctx)
	},
	"telemetry": func(err error, args []arg) error {
		return telemetrykeys.WithTelemetry(err, strfyList(args)...)
	},
	"hint": func(err error, args []arg) error {
		return hintdetail.WithHint(err, strfy(args))
	},
	"detail": func(err error, args []arg) error {
		return hintdetail.WithDetail(err, strfy(args))
	},
	"safedetails": func(err error, args []arg) error {
		return safedetails.WithSafeDetails(err, "safe %s", safedetails.Safe(strfy(args)))
	},
	"assertwrap": func(err error, args []arg) error {
		return errutil.NewAssertionErrorWithWrappedErrf(err, "assertwrap %s", redact.Safe(strfy(args)))
	},
	"opaque": func(err error, _ []arg) error {
		return errbase.DecodeError(context.Background(), errbase.EncodeError(context.Background(), err))
	},
}

func TestDatadriven(t *testing.T) {
	var events []*sentrygo.Event

	client, err := sentrygo.NewClient(
		sentrygo.ClientOptions{
			Transport: interceptingTransport{
				SendFunc: func(event *sentrygo.Event) {
					events = append(events, event)
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	sentrygo.CurrentHub().BindClient(client)

	datadriven.Walk(t, testPath, func(t *testing.T, path string) {
		datadriven.RunTest(t, path,
			func(t *testing.T, d *datadriven.TestData) string {
				if d.Cmd != "run" {
					d.Fatalf(t, "unknown directive: %s", d.Cmd)
				}
				pos := d.Pos
				var resultErr error

				lines := strings.Split(d.Input, "\n")
				for i, line := range lines {
					if short := strings.TrimSpace(line); short == "" || strings.HasPrefix(short, "#") {
						continue
					}
					d.Pos = fmt.Sprintf("\n%s: (+%d)", pos, i+1)

					var err error
					d.Cmd, d.CmdArgs, err = datadriven.ParseLine(line)
					if err != nil {
						d.Fatalf(t, "%v", err)
					}
					var c commandFn
					if resultErr == nil {
						c = leafCommands[d.Cmd]
					} else {
						c = wrapCommands[d.Cmd]
					}
					if c == nil {
						d.Fatalf(t, "unknown command: %s", d.Cmd)
					}
					resultErr = c(resultErr, d.CmdArgs)
				}
				if resultErr == nil {
					d.Fatalf(t, "run block did not construct an error")
				}

				events = nil
				if eventID := errorssentry.ReportError(resultErr); eventID == "" {
					d.Fatalf(t, "Sentry eventID is empty")
				}
				if len(events) != 1 {
					d.Fatalf(t, "expected one Sentry event, got %d", len(events))
				}

				return formatEvent(events[0])
			})
	})
}

func TestCleanFramePathNormalizesArchitectureSpecificAssembly(t *testing.T) {
	for _, tc := range []struct {
		in       string
		expected string
	}{
		{in: "runtime/asm_amd64.s", expected: "runtime/asm_GOARCH.s"},
		{in: "runtime/asm_arm64.s", expected: "runtime/asm_GOARCH.s"},
	} {
		if actual := cleanFramePath(tc.in); actual != tc.expected {
			t.Fatalf("expected %q, got %q", tc.expected, actual)
		}
	}
}

func formatEvent(event *sentrygo.Event) string {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "== Message payload\n%s\n", cleanReport(event.Message))

	tagNames := make([]string, 0, len(event.Tags))
	for k := range event.Tags {
		tagNames = append(tagNames, k)
	}
	sort.Strings(tagNames)
	for _, k := range tagNames {
		fmt.Fprintf(&buf, "== Tag %q\n%s\n", k, event.Tags[k])
	}

	extraNames := make([]string, 0, len(event.Extra))
	for k := range event.Extra {
		extraNames = append(extraNames, k)
	}
	sort.Strings(extraNames)
	for _, k := range extraNames {
		extra := strings.TrimSpace(fmt.Sprint(event.Extra[k]))
		fmt.Fprintf(&buf, "== Extra %q\n%s\n", k, cleanReport(extra))
	}

	for i, exc := range event.Exception {
		fmt.Fprintf(&buf, "== Exception %d\n", i+1)
		fmt.Fprintf(&buf, "Module: %q\n", exc.Module)
		fmt.Fprintf(&buf, "Type: %q\n", cleanReport(exc.Type))
		fmt.Fprintf(&buf, "Title: %q\n", cleanReport(exc.Value))
		if exc.Stacktrace == nil {
			buf.WriteString("(NO STACKTRACE)\n")
			continue
		}
		for _, f := range exc.Stacktrace.Frames {
			fmt.Fprintf(&buf, "%s:<line>:\n", cleanFramePath(f.Filename))
			fmt.Fprintf(&buf, "   (%s) %s()\n", cleanFramePath(f.Module), cleanReport(f.Function))
		}
	}

	return buf.String()
}

func cleanReport(s string) string {
	s = fileref.ReplaceAllString(s, "<path>:<line>")
	s = libref.ReplaceAllString(s, "<path>")
	s = strings.ReplaceAll(s, "\t", "<tab>")
	s = funcNN.ReplaceAllString(s, "funcNN")
	return s
}

func cleanFramePath(s string) string {
	s = strings.TrimPrefix(cleanReport(s), "<path>/")
	if i := strings.Index(s, "/github.com/"); i >= 0 {
		s = s[i+1:]
	}
	s = asmref.ReplaceAllString(s, "runtime/asm_GOARCH.s")
	if s == "." {
		return ""
	}
	return s
}

var fileref = regexp.MustCompile(`[a-zA-Z0-9._/@-]+\.(?:go|s):\d+`)

var funcNN = regexp.MustCompile(`func\d+`)

var asmref = regexp.MustCompile(`runtime/asm_[a-z0-9]+\.s`)

var libroot = func() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}()

var libref = regexp.MustCompile(regexp.QuoteMeta(libroot))

func strfy(args []arg) string {
	var out strings.Builder
	sp := ""
	for _, arg := range args {
		out.WriteString(sp)
		if len(arg.Vals) == 0 {
			out.WriteString(arg.Key)
		} else {
			out.WriteString(strings.Join(arg.Vals, " "))
		}
		sp = "\n"
	}
	return out.String()
}

func strfyList(args []arg) []string {
	var out []string
	for _, arg := range args {
		if len(arg.Vals) == 0 {
			out = append(out, arg.Key)
		} else {
			out = append(out, strings.Join(append([]string{arg.Key}, arg.Vals...), " "))
		}
	}
	return out
}

// interceptingTransport is an implementation of sentry.Transport that
// delegates calls to the send function contained within.
type interceptingTransport struct {
	SendFunc func(event *sentrygo.Event)
}

var _ sentrygo.Transport = &interceptingTransport{}

func (it interceptingTransport) Flush(time.Duration) bool {
	return true
}

func (it interceptingTransport) Configure(sentrygo.ClientOptions) {
}

func (it interceptingTransport) SendEvent(event *sentrygo.Event) {
	it.SendFunc(event)
}
