package cascadingconfig_test

import "flag"

// cctest registers no flags (DL-274), so this package's tests define -update to rewrite their golden files.
var _ = flag.Bool("update", false, "rewrite golden files")
