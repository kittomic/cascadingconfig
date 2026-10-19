package cascadingconfig

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// orderSlot orders the files found in one slot. files have Path and Format set, and order lists the registered
// decoders by format precedence, highest first; within a format, extensions rank as the decoder lists them.
//
// It returns the slot's layers lowest-ranked first, which is their merge order. Under ConflictFirst, all but the
// highest-ranked are LayerIgnored, each with a WarnFormatConflict warning. Under ConflictFail, more than one file is a
// *ConflictError.
func orderSlot(files []Layer, order []Decoder, policy FormatConflict) ([]Layer, error) {
	ranked := slices.Clone(files) // highest-ranked first
	for i := range ranked {
		ranked[i].Status = LayerLoaded
		ranked[i].Warnings = slices.Clone(ranked[i].Warnings)
	}
	slices.SortStableFunc(ranked, func(a, b Layer) int {
		fa, ea := rank(a, order)
		fb, eb := rank(b, order)
		if fa != fb {
			return fa - fb
		}
		return ea - eb
	})

	if len(ranked) > 1 {
		switch policy {
		case ConflictFirst:
			top := ranked[0].Path
			for i := 1; i < len(ranked); i++ {
				l := &ranked[i]
				l.Status = LayerIgnored
				l.Reason = fmt.Sprintf("%s, in the same slot, ranks higher", top)
				l.Warnings = append(l.Warnings, Warning{
					Code:    WarnFormatConflict,
					Path:    l.Path,
					Message: fmt.Sprintf("%s ignored: %s, in the same slot, ranks higher and was loaded", l.Path, top),
				})
			}
		case ConflictFail:
			names := make([]string, len(ranked))
			for i, l := range ranked {
				names[i] = l.Path
			}
			return nil, &ConflictError{
				BaseError: BaseError{
					Op: "load", Path: names[0], Scope: string(ranked[0].Scope),
					Err: fmt.Errorf("one slot matches %d files, and only one is allowed: %s",
						len(names), strings.Join(names, ", ")),
				},
				Files: names,
			}
		}
	}

	slices.Reverse(ranked)
	return ranked, nil
}

// rank returns a file's format rank and extension rank, lowest first. A format or extension that isn't listed ranks
// last.
func rank(l Layer, order []Decoder) (format, ext int) {
	format, ext = len(order), 0
	for i, d := range order {
		if d.Format() != l.Format {
			continue
		}
		exts := d.Extensions()
		ext = len(exts)
		if j := slices.Index(exts, strings.TrimPrefix(filepath.Ext(l.Path), ".")); j >= 0 {
			ext = j
		}
		return i, ext
	}
	return format, ext
}
