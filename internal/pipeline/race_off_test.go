//go:build !race

package pipeline_test

// raceEnabled reports whether the race detector is on, which slows the
// code down too much for timing tests.
const raceEnabled = false
