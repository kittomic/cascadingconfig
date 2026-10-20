package cascadingconfig

import "context"

// WithLayerTransform adds a function that runs on each layer that merges, after decoding and before its include key and
// _unset are read (DL-36, DL-193). It gets a copy of the tree. A nil result merges nothing. An error, or a tree with a
// non-canonical value, is a *TransformError naming the file.
func WithLayerTransform(f func(ctx context.Context, l Layer, t Tree) (Tree, error)) Option {
	_ = f // stub
	return optionFunc(func(*config) {})
}

// WithResultTransform adds a function that runs once on the merged result, before the RequireConfig checks. A nil
// result is an empty one. An error, or a tree with a non-canonical value, is a *TransformError.
func WithResultTransform(f func(ctx context.Context, t Tree) (Tree, error)) Option {
	_ = f // stub
	return optionFunc(func(*config) {})
}
