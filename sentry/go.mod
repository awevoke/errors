module github.com/cockroachdb/errors/sentry

go 1.23.0

toolchain go1.23.8

require (
	github.com/cockroachdb/errors v0.0.0
	github.com/getsentry/sentry-go v0.27.0
)

replace github.com/cockroachdb/errors => ..
