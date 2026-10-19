package cascadingconfig

// orderSlot orders the files found in one slot. files have Path and Format set, and order lists the registered
// decoders by format precedence, highest first; within a format, extensions rank as the decoder lists them.
//
// It returns the slot's layers lowest-ranked first, which is their merge order. Under ConflictFirst, all but the
// highest-ranked are LayerIgnored, each with a WarnFormatConflict warning. Under ConflictFail, more than one file is a
// *ConflictError.
func orderSlot(files []Layer, order []Decoder, policy FormatConflict) ([]Layer, error) {
	_, _, _ = files, order, policy // stub
	return nil, nil
}
