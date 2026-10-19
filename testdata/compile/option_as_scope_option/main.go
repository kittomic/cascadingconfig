// want: cannot use cc.WithScopes("flags") (value of interface type cascadingconfig.Option) as cascadingconfig.ScopeOption value
package main

import cc "github.com/kittomic/cascadingconfig"

func main() {
	_ = cc.WithScope("flags", cc.Values(cc.Tree{}), cc.WithScopes("flags"))
}
