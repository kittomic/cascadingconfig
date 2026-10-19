// want: cannot use m (variable of interface type cascadingconfig.Matcher) as cascadingconfig.Option value
package main

import cc "github.com/kittomic/cascadingconfig"

func main() {
	var m cc.Matcher
	_, _ = cc.New("myapp", m)
}
