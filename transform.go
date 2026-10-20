package cascadingconfig

import (
	"context"
	"fmt"

	"github.com/kittomic/cascadingconfig/internal/tree"
)

// WithLayerTransform adds a function that runs on each layer that merges, after decoding and before its include key and
// _unset are read (DL-36, DL-193). It gets a copy of the tree. A nil result merges nothing. An error, or a tree with a
// non-canonical value, is a *TransformError naming the file.
func WithLayerTransform(f func(ctx context.Context, l Layer, t Tree) (Tree, error)) Option {
	return optionFunc(func(c *config) {
		if f == nil {
			c.fail("WithLayerTransform", "a nil function")
			return
		}
		c.layerTransforms = append(c.layerTransforms, f)
	})
}

// WithResultTransform adds a function that runs once on the merged result, before the RequireConfig checks. A nil
// result is an empty one. An error, or a tree with a non-canonical value, is a *TransformError.
func WithResultTransform(f func(ctx context.Context, t Tree) (Tree, error)) Option {
	return optionFunc(func(c *config) {
		if f == nil {
			c.fail("WithResultTransform", "a nil function")
			return
		}
		c.resultTransforms = append(c.resultTransforms, f)
	})
}

// transformLayer runs the layer transforms on e's tree, in order, each on its own copy.
func (ld *loading) transformLayer(e *entry) error {
	t := e.tree
	for _, f := range ld.c.layerTransforms {
		l := e.layer
		l.Warnings = cloneWarnings(l.Warnings)
		next, err := f(ld.ctx, l, tree.CopyMap(t))
		if err == nil {
			next, err = canonicalResult(next)
		}
		if err != nil {
			return &TransformError{BaseError: BaseError{
				Op: "transform", Path: e.layer.Path, Scope: string(e.layer.Scope), Err: err,
			}}
		}
		t = next
	}
	e.tree = t
	return nil
}

// transformResult runs the result transforms on the merged tree, in order, each on its own copy.
func (ld *loading) transformResult(t Tree) (Tree, error) {
	for _, f := range ld.c.resultTransforms {
		next, err := f(ld.ctx, tree.CopyMap(t))
		if err == nil {
			next, err = canonicalResult(next)
		}
		if err != nil {
			return nil, &TransformError{BaseError: BaseError{Op: "transform", Err: err}}
		}
		t = next
	}
	return t, nil
}

// canonicalResult checks a transform's tree, which may be nil for an empty one.
func canonicalResult(t Tree) (Tree, error) {
	if t == nil {
		return Tree{}, nil
	}
	if err := checkCanonical(t, nil); err != nil {
		return nil, fmt.Errorf("the transform returned %w", err)
	}
	return t, nil
}
