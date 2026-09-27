// Package buildinfo reports the version and project identity of a Chowki build.
//
// The Makefile sets every value at link time with -ldflags -X, from git and
// project.env. The identity defaults in project.go are generated from
// project.env by `make sync`, so a plain `go build` or `go test` still reports
// the right identity. Code outside this package must never hard-code the
// project domain or repository URLs; it asks this package instead.
package buildinfo
