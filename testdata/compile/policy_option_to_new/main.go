// want: cannot use p (variable of interface type cascadingconfig.PolicyOption) as cascadingconfig.Option value
package main

import cc "github.com/kittomic/cascadingconfig"

func main() {
	var p cc.PolicyOption
	_, _ = cc.New("myapp", p)
}
