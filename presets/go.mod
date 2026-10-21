module github.com/kittomic/cascadingconfig/presets

go 1.26.0

// Until the first release the other modules can't be fetched, because the repository is private, so this module builds
// against the repository's copies (DL-162, DL-249). M11 replaces these with the released versions.
replace github.com/kittomic/cascadingconfig => ../

require github.com/kittomic/cascadingconfig v0.0.0-00010101000000-000000000000

require github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
