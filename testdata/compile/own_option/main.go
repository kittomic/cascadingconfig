// want: cannot use myOption{} (value of struct type myOption) as cascadingconfig.Option value
package main

import cc "github.com/kittomic/cascadingconfig"

// myOption tries to implement the sealed Option interface from outside the package.
type myOption struct{}

func (myOption) Apply() {}

func main() {
	_, _ = cc.New("myapp", myOption{})
}
