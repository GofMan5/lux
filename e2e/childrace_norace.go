//go:build !race

package e2e

// childRaceFlag is empty when the suite itself runs without -race: the
// child is then built plain too, so plain local runs of the e2e suite do
// not require a C toolchain or pay the race-detector overhead.
const childRaceFlag = ""
