package conformance_test

import (
	"os"
	"testing"
)

// The package's tests read files outside its module, which go test's cache doesn't check. Reading CC_TEST_INPUTS,
// which scripts/each-module.sh sets to a hash of the repository, puts the repository's state in the cache key, so a
// cached result is used only while nothing in the repository has changed (DL-288).
func TestRepositoryInputs(t *testing.T) {
	t.Parallel()
	_ = os.Getenv("CC_TEST_INPUTS")
}
