package cascadingconfig_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	cc "github.com/kittomic/cascadingconfig"
)

// level is an app's own type, which a decode hook fills from a name.
type level int

// levelHook turns "debug" or "info" into a level, and fails on any other string headed for one.
func levelHook(_, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeFor[level]() {
		return data, nil
	}
	switch data {
	case "debug":
		return level(1), nil
	case "info":
		return level(2), nil
	}
	return nil, errors.New("not a level")
}

// DL-278: a hook added with WithDecodeHook fills an app's own type, and its error is the key's *DecodeError.
func TestWithDecodeHook(t *testing.T) {
	var v struct {
		Lvl level `koanf:"lvl"`
	}
	if err := decodeValues(t, cc.Tree{"lvl": "debug"}, &v, cc.WithDecodeHook(levelHook)); err != nil || v.Lvl != 1 {
		t.Errorf("decoded %d, %v; want 1", v.Lvl, err)
	}
	err := decodeValues(t, cc.Tree{"lvl": "loud"}, &v, cc.WithDecodeHook(levelHook))
	if des := decodeErrors(t, err); len(des) != 1 || des[0].Key.String() != "lvl" {
		t.Errorf("a hook's error: %v, want one *DecodeError for lvl", err)
	}
}

// DL-278: added hooks run before the default hooks, which still apply, and run in the order they were added.
func TestDecodeHookOrder(t *testing.T) {
	forever := func(_, to reflect.Type, data any) (any, error) {
		if to == reflect.TypeFor[time.Duration]() && data == "forever" {
			return time.Duration(1 << 62), nil
		}
		return data, nil
	}
	var d struct {
		A time.Duration `koanf:"a"`
		B time.Duration `koanf:"b"`
	}
	if err := decodeValues(t, cc.Tree{"a": "forever", "b": "7d"}, &d, cc.WithDecodeHook(forever)); err != nil ||
		d.A != 1<<62 || d.B != 7*24*time.Hour {
		t.Errorf("decoded %v and %v, %v; want the hook's value, and the default hook's 168h", d.A, d.B, err)
	}

	rewrite := func(from, to string) cc.DecodeHook {
		return func(_, _ reflect.Type, data any) (any, error) {
			if data == from {
				return to, nil
			}
			return data, nil
		}
	}
	var s struct {
		S string `koanf:"s"`
	}
	if err := decodeValues(t, cc.Tree{"s": "x"}, &s,
		cc.WithDecodeHook(rewrite("x", "y")), cc.WithDecodeHook(rewrite("y", "z"))); err != nil || s.S != "z" {
		t.Errorf("decoded %q, %v; want z, from the first hook then the second", s.S, err)
	}
}

// DL-278: by default a string fills a number, as koanf does; WithoutWeakTyping makes that a *DecodeError.
func TestWithoutWeakTyping(t *testing.T) {
	var v struct {
		Port int `koanf:"port"`
	}
	if err := decodeValues(t, cc.Tree{"port": "8080"}, &v); err != nil || v.Port != 8080 {
		t.Errorf("weak typing: %d, %v; want 8080", v.Port, err)
	}
	v.Port = 0
	err := decodeValues(t, cc.Tree{"port": "8080"}, &v, cc.WithoutWeakTyping())
	if des := decodeErrors(t, err); len(des) != 1 || des[0].Key.String() != "port" {
		t.Errorf("without weak typing: %v, want one *DecodeError for port", err)
	}
}

// DL-278: with WithZeroFields, a null clears a value set in Go; _unset still keeps it, because the key is absent.
// Without it, a null leaves the value as it was.
func TestWithZeroFields(t *testing.T) {
	files := map[string]string{
		sysFile:  `{"host": "sys", "port": 1}`,
		projFile: `{"host": null, "_unset": ["port"]}`,
	}
	type server struct {
		Host string `koanf:"host"`
		Port int    `koanf:"port"`
	}
	for _, c := range []struct {
		name string
		opts []cc.Option
		want server
	}{
		{"without", nil, server{Host: "go", Port: 9}},
		{"with", []cc.Option{cc.WithZeroFields()}, server{Host: "", Port: 9}},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := server{Host: "go", Port: 9}
			if err := loadOK(t, files, c.opts...).Decode(&v); err != nil || v != c.want {
				t.Errorf("decoded %+v, %v; want %+v", v, err, c.want)
			}
		})
	}
}

// DL-278: a nil hook is an *OptionError from WithDecodeHook.
func TestWithDecodeHookNil(t *testing.T) {
	_, err := cc.NewWithPreset("myapp", cc.MiseLayout, cc.WithDecodeHook(nil))
	for _, e := range cc.Errors(err) {
		var oe *cc.OptionError
		if errors.As(e, &oe) && oe.Op == "WithDecodeHook" {
			return
		}
	}
	t.Errorf("WithDecodeHook(nil): %v, want an *OptionError from WithDecodeHook", err)
}
