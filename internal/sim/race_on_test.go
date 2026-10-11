//go:build race

package sim

// raceEnabled is true when the tests are built with the race detector, which makes them
// roughly ten times slower: the cost grids shrink (see costSeeds, thin).
const raceEnabled = true
