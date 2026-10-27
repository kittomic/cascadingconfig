package compat_test

import (
	"reflect"
	"testing"

	cc "github.com/kittomic/cascadingconfig"
	ccjson "github.com/kittomic/cascadingconfig/format/json"
	"github.com/kittomic/cascadingconfig/format/toml"
	"github.com/kittomic/cascadingconfig/format/yaml"
)

// The same document in TOML, YAML and JSON gives an identical tree.
func TestAcrossFormats(t *testing.T) {
	t.Parallel()
	docs := []struct {
		dec cc.Decoder
		src string
	}{
		{toml.Decoder(), `
name = "app"
port = 8080
ratio = 0.5
big = 1e3
debug = true
tags = ["a", "b"]
started = 1979-05-27T07:32:00Z

[server]
host = "localhost"
ports = [80, 443]

[[backends]]
url = "http://a"

[[backends]]
url = "http://b"
weight = 2
`},
		{yaml.Decoder(), `
name: app
port: 8080
ratio: 0.5
big: 1e3
debug: true
tags: [a, b]
started: 1979-05-27T07:32:00Z
server:
  host: localhost
  ports: [80, 443]
backends:
  - url: http://a
  - url: http://b
    weight: 2
`},
		{ccjson.Decoder(), `{
  "name": "app", "port": 8080, "ratio": 0.5, "big": 1e3, "debug": true, "tags": ["a", "b"],
  "started": "1979-05-27T07:32:00Z",
  "server": {"host": "localhost", "ports": [80, 443]},
  "backends": [{"url": "http://a"}, {"url": "http://b", "weight": 2}]
}`},
	}
	var first cc.Tree
	for i, d := range docs {
		doc, err := cc.DecodeDocument(d.dec, "config."+d.dec.Extensions()[0], []byte(d.src))
		if err != nil {
			t.Fatalf("%s: %v", d.dec.Format(), err)
		}
		if i == 0 {
			first = doc.Tree
			continue
		}
		if !reflect.DeepEqual(doc.Tree, first) {
			t.Errorf("%s gave a different tree:\n%#v\n%s gave\n%#v", d.dec.Format(), doc.Tree, docs[0].dec.Format(), first)
		}
	}
}
