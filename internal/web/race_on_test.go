//go:build race

package web

// raceEnabled reports whether the race detector is on, which slows the
// code down too much for timing tests.
const raceEnabled = true
