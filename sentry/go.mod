module github.com/cockroachdb/errors/sentry

go 1.23.0

toolchain go1.23.8

require (
	github.com/cockroachdb/datadriven v1.0.2
	github.com/cockroachdb/errors v1.12.0
	github.com/cockroachdb/logtags v0.0.0-20230118201751-21c54148d20b
	github.com/cockroachdb/redact v1.1.5
	github.com/getsentry/sentry-go v0.27.0
	github.com/kr/pretty v0.3.1
	github.com/pkg/errors v0.9.1
)

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/planetscale/vtprotobuf v0.6.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.9.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
)

replace github.com/cockroachdb/errors => ..
