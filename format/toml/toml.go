package toml

import (
	"errors"
	"fmt"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/kittomic/cascadingconfig"
)

// Decoder returns the TOML decoder.
//
// It accepts whatever the pinned go-toml accepts, which includes TOML 1.1 syntax. A duplicate key is an error, as the
// TOML spec requires. Offset date-times become RFC 3339 text, and local date-times, dates and times their TOML text, so
// every date in the tree is a string. A file with no keys is empty.
//
// Use it through cascadingconfig.DecodeDocument, which also strips a byte-order mark and rejects input that isn't
// UTF-8.
func Decoder() cascadingconfig.Decoder {
	return decoder{}
}

type decoder struct{}

func (decoder) Format() string { return "toml" }

func (decoder) Extensions() []string { return []string{"toml"} }

func (decoder) MediaTypes() []string { return []string{"application/toml"} }

// Decode parses data. go-toml's errors carry their position and a short message that doesn't quote the file.
func (decoder) Decode(data []byte) (cascadingconfig.Document, error) {
	m, err := parse(data)
	if err != nil {
		return cascadingconfig.Document{}, err
	}
	if len(m) == 0 {
		return cascadingconfig.Document{Empty: true}, nil
	}
	t, err := canonical(m)
	if err != nil {
		return cascadingconfig.Document{}, err
	}
	return cascadingconfig.Document{Tree: t.(map[string]any)}, nil
}

// parse runs go-toml. A panic inside it is returned as an error, so a hostile file can't crash the app; this guard
// covers only go-toml's call, so a panic in this package's own code still surfaces as a bug.
func parse(data []byte) (m map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			m, err = nil, errors.New("the TOML parser failed on this file")
		}
	}()
	err = toml.Unmarshal(data, &m)
	return m, err
}

// canonical turns go-toml's values into canonical ones: its date and time types become text.
func canonical(v any) (any, error) {
	switch v := v.(type) {
	case map[string]any:
		for k, e := range v {
			c, err := canonical(e)
			if err != nil {
				return nil, err
			}
			v[k] = c
		}
		return v, nil
	case []any:
		for i, e := range v {
			c, err := canonical(e)
			if err != nil {
				return nil, err
			}
			v[i] = c
		}
		return v, nil
	case time.Time:
		return v.Format(time.RFC3339Nano), nil
	case toml.LocalDateTime:
		return v.String(), nil
	case toml.LocalDate:
		return v.String(), nil
	case toml.LocalTime:
		return v.String(), nil
	case string, bool, int64, float64:
		return v, nil
	default:
		return nil, fmt.Errorf("go-toml returned a %T, which this decoder doesn't convert", v)
	}
}
