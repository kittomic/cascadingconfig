// Package cctest gives a cascadingconfig loader a world of its own, so an app's config tests don't depend on the
// machine they run on: a developer's ~/.config or a CI runner's environment never changes the result.
//
// [NewFS] is an in-memory filesystem, passed with cascadingconfig.WithFileSystem; [Host], passed with [WithHost],
// replaces the OS, the home and working directories, the user ID and the environment variables. For a watcher,
// [NewClock] and [NewNotifier] let a test move time and send file events itself, so it never sleeps. [Golden] compares
// output with a file in testdata, and rewrites it with CCTEST_UPDATE=1 or the test package's own -update flag; cctest
// registers no flags.
//
// See [Testing your app].
//
// [Testing your app]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/testing.md
package cctest
