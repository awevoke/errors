module github.com/cockroachdb/errors/sentry

go 1.25.0

require (
	github.com/cockroachdb/datadriven v1.0.2
	github.com/cockroachdb/errors v1.12.0
	github.com/cockroachdb/logtags v0.0.0-20241215232642-bb51bb14a506
	github.com/cockroachdb/redact v1.1.8
	github.com/getsentry/sentry-go v0.47.0
	github.com/kr/pretty v0.3.1
	github.com/pkg/errors v0.9.1
)

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.15.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/cockroachdb/errors => ..
