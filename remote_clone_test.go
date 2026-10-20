package cascadingconfig_test

import "testing"

// Describe returns a fresh report each time, so changing one report's Layer.Remote leaves later reports alone.
func TestRemoteReportIsCopied(t *testing.T) {
	const ref = "https://h.example/a.json"
	res := remOK(t, newFake("https").serve(ref, `{}`), []string{ref})
	layerAt(t, res.Describe(), ref).Remote.Identity = "changed"
	if got := layerAt(t, res.Describe(), ref).Remote.Identity; got == "changed" {
		t.Errorf("a change to one report's Remote reached the next report")
	}
}
