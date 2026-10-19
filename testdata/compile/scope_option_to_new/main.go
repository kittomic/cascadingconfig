// want: cannot use cc.After("user") (value of interface type cascadingconfig.ScopeOption) as cascadingconfig.Option value
package main

import cc "github.com/kittomic/cascadingconfig"

func main() {
	_, _ = cc.New("myapp", cc.After("user"))
}
