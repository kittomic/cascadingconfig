// want: cannot use cc.Values(cc.Tree{}) (value of interface type cascadingconfig.ScopeKind) as cascadingconfig.Option value
package main

import cc "github.com/kittomic/cascadingconfig"

func main() {
	_, _ = cc.New("myapp", cc.Values(cc.Tree{}))
}
