# Third-party test suites

The directories here hold other projects' test suites, vendored unmodified so the conformance tests can run offline
(DL-269). They are **not** covered by this repository's Apache License 2.0. Each is under its own licence, kept beside
it in the directory, and each directory's `SOURCE` file names the repository and the version it came from.

| Directory          | Project                                                       | Licence                                         |
| ------------------ | ------------------------------------------------------------- | ----------------------------------------------- |
| `toml-test/`       | [toml-test](https://github.com/toml-lang/toml-test)           | MIT, Copyright (c) 2018 TOML authors            |
| `yaml-test-suite/` | [yaml-test-suite](https://github.com/yaml/yaml-test-suite)    | MIT, Copyright (c) 2016-2020 Ingy döt Net       |
| `JSONTestSuite/`   | [JSONTestSuite](https://github.com/nst/JSONTestSuite)         | MIT, Copyright (c) 2016 Nicolas Seriot          |

**YAML specification examples.** The yaml-test-suite cases named "Spec Example …" reproduce examples from the
[YAML 1.2 specification](https://yaml.org/spec/1.2.2/) verbatim. The specification's own notice reads: "Copyright
2001-2009 by Oren Ben-Kiki, Clark Evans, Ingy döt Net. This document may be freely copied, provided it is not modified."
The examples here are unmodified.

**Keeping them unmodified.** `scripts/refresh-conformance.sh` replaces a suite's files, licence included, from its
pinned version. Don't edit the files by hand; record a case's outcome in `../outcomes/` instead.
