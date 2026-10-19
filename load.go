package cascadingconfig

import "context"

// Load discovers, reads, decodes and merges every selected scope, and returns the result. Every problem found is
// returned, joined.
func (l *Loader) Load(ctx context.Context) (*Result, error) {
	_ = ctx // stub
	return &Result{}, nil
}
