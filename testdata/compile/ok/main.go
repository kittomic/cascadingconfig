// This program must build: it checks that the other programs fail only because of their misuse.
package main

import cc "github.com/kittomic/cascadingconfig"

func main() {
	_, _ = cc.New("myapp",
		cc.WithScope("flags", cc.Values(cc.Tree{}), cc.After("x"), cc.Trust(cc.TrustUser)),
		cc.WithScopes("flags"),
	)
}
