package cascadingconfig

import "errors"

// errNotImplemented is returned by API that is declared but not yet implemented.
//
// It exists only while milestones are in progress: the acceptance tests are written against the declared API, and
// shown to fail, before the implementation lands.
var errNotImplemented = errors.New("cascadingconfig: not implemented")
