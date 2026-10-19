// want: cannot use w (variable of interface type cascadingconfig.WatchOption) as cascadingconfig.Option value
package main

import cc "github.com/kittomic/cascadingconfig"

func main() {
	var w cc.WatchOption
	_, _ = cc.New("myapp", w)
}
