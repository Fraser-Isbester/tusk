// Package knownbug marks specs that reproduce a confirmed, unfixed bug. Such
// specs assert the correct behavior and are skipped by default so the suite
// stays green; set TUSK_KNOWN_BUGS=1 to run them (they should fail until fixed,
// and the skip should be deleted in the fixing PR).
package knownbug

import (
	"os"

	"github.com/onsi/ginkgo/v2"
)

const Env = "TUSK_KNOWN_BUGS"

// Skip skips the current spec with reason unless TUSK_KNOWN_BUGS is set.
func Skip(reason string) {
	if os.Getenv(Env) == "" {
		ginkgo.Skip("known bug: "+reason, 1)
	}
}
