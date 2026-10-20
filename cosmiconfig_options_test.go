package cascadingconfig_test

import (
	"testing"

	cc "github.com/kittomic/cascadingconfig"
)

// DL-139 to DL-141: resolution, walk-stop and file-format options that can only be mistakes.
func TestCosmiconfigOptionErrors(t *testing.T) {
	runOptionErrorCases(t, []optionErrorCase{
		{name: "an unknown resolution", opts: []cc.Option{scope, cc.WithResolution(cc.Resolution(9))}, ops: []string{"WithResolution"}},
		{name: "an unknown strategy", opts: []cc.Option{scope, cc.WithSearchStrategy(cc.SearchStrategy(9))}, ops: []string{"WithSearchStrategy"}},
		{name: "a stop dir without StopAtHome", opts: []cc.Option{scope, cc.WithWalkStop(cc.StopAtStart), cc.WithStopDir("/x")}, ops: []string{"WithStopDir"}},
		{name: "a stop dir with SearchProject", opts: []cc.Option{scope, cc.WithSearchStrategy(cc.SearchProject), cc.WithRootMarker(".git"), cc.WithStopDir("/x")}, ops: []string{"WithStopDir"}},
		{name: "StopAtMarker without a marker", opts: []cc.Option{scope, cc.WithWalkStop(cc.StopAtMarker)}, ops: []string{"WithRootMarker"}},
		{name: "an empty stop dir", opts: []cc.Option{scope, cc.WithStopDir("")}, ops: []string{"WithStopDir"}},
		{name: "an empty marker", opts: []cc.Option{scope, cc.WithRootMarker("")}, ops: []string{"WithRootMarker"}},
		{name: "a marker with a separator", opts: []cc.Option{scope, cc.WithRootMarker("a/b")}, ops: []string{"WithRootMarker"}},
		{name: "a nil matcher", opts: []cc.Option{scope, cc.WithFileFormat(nil, "json")}, ops: []string{"WithFileFormat"}},
		{name: "no formats", opts: []cc.Option{scope, cc.WithFileFormat(cc.Extension("conf"))}, ops: []string{"WithFileFormat"}},
		{name: "no extensions", opts: []cc.Option{scope, cc.WithFileFormat(cc.Extension(), "json")}, ops: []string{"Extension"}},
		{name: "an empty extension", opts: []cc.Option{scope, cc.WithFileFormat(cc.Extension(""), "json")}, ops: []string{"Extension"}},
		{name: "an empty file name", opts: []cc.Option{scope, cc.WithFileFormat(cc.FileName(""), "json")}, ops: []string{"FileName"}},
		{name: "a file name with a separator", opts: []cc.Option{scope, cc.WithFileFormat(cc.FileName("a/b"), "json")}, ops: []string{"FileName"}},
		{name: "an empty glob", opts: []cc.Option{scope, cc.WithFileFormat(cc.Glob(""), "json")}, ops: []string{"Glob"}},
		{name: "a bad regexp", opts: []cc.Option{scope, cc.WithFileFormat(cc.Regexp("("), "json")}, ops: []string{"Regexp"}},
		{name: "a nil function", opts: []cc.Option{scope, cc.WithFileFormat(cc.MatchFunc(nil), "json")}, ops: []string{"MatchFunc"}},
	})
}
