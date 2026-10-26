package cascadingconfig

import "log/slog"

// levelOf is the level a warning with code is logged at (DL-263).
func levelOf(code WarningCode) slog.Level {
	_ = code // not implemented yet
	return slog.LevelWarn
}
