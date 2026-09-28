// Package doctor checks that chowki serve can start and work with a
// configuration, and says what to fix. The checks change nothing: they
// don't create or migrate the database, and they call no provider.
package doctor
