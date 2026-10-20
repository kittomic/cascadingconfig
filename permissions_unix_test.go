//go:build unix

package cascadingconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/cctest"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
)

// DL-191: on a real filesystem, StrictModes reads the mode and the owner from the OS. A group-writable file is ignored
// with a warning, and a file owned by the current user with mode 0644 loads.
func TestOSStrictModes(t *testing.T) {
	dir := t.TempDir()
	good, bad := filepath.Join(dir, "good.json"), filepath.Join(dir, "bad.json")
	writeFile(t, good, `{"a": 1}`)
	writeFile(t, bad, `{"b": 2}`)
	if err := os.Chmod(bad, 0o664); err != nil {
		t.Fatal(err)
	}
	h := testHost(nil)
	h.UID = os.Getuid()
	l, err := cc.New("myapp", cc.WithDecoder(ccjson.Decoder()), cctest.WithHost(h),
		cc.WithScope("d", cc.FixedDir(dir, cc.Pattern{Template: "good.json"}, cc.Pattern{Template: "bad.json"})),
		cc.WithScopes("d"),
		cc.WithDefaultPolicy(cc.FilePermissions(cc.StrictModes)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := l.Load(t.Context())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantTreeJSON(t, res.Tree(), `{"a": 1}`)
	if lb := layerAt(t, res.Describe(), bad); lb.Status != cc.LayerIgnored || !hasWarningCode(lb.Warnings, cc.WarnFilePermissions) {
		t.Errorf("bad.json = %+v, want LayerIgnored with a file-permissions warning", lb)
	}
}
