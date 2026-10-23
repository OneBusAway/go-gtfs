//go:build race

package gtfs

// raceEnabled reports whether tests run under the race detector, which
// changes heap usage enough to make memory measurements meaningless.
const raceEnabled = true
