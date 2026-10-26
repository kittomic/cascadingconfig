package cascadingconfig

import (
	"log/slog"
	"testing"
)

// DL-263: each warning code's level. Error is only for a watch handler's panic, which has no code.
func TestWarningLevels(t *testing.T) {
	for code, want := range map[WarningCode]slog.Level{
		WarnUntrusted:      slog.LevelInfo,
		WarnFormatConflict: slog.LevelInfo,
		WarnNoHome:         slog.LevelInfo,

		WarnUnreadable:          slog.LevelWarn,
		WarnNotAFile:            slog.LevelWarn,
		WarnBrokenSymlink:       slog.LevelWarn,
		WarnFilePermissions:     slog.LevelWarn,
		WarnDuplicateKey:        slog.LevelWarn,
		WarnReservedKey:         slog.LevelWarn,
		WarnUnknownKey:          slog.LevelWarn,
		WarnEnvVarSkipped:       slog.LevelWarn,
		WarnEnvVarClash:         slog.LevelWarn,
		WarnBootstrapUnknownKey: slog.LevelWarn,
		WarnBootstrapKeyDropped: slog.LevelWarn,
		WarnStaleInclude:        slog.LevelWarn,
		WarnIncludeCache:        slog.LevelWarn,
		WarnWatchFailed:         slog.LevelWarn,
		WarnReloadFailed:        slog.LevelWarn,

		// A code from another module, such as format/yaml's, is a Warn.
		"yaml-version":   slog.LevelWarn,
		"from-elsewhere": slog.LevelWarn,
	} {
		if got := levelOf(code); got != want {
			t.Errorf("levelOf(%q) = %v, want %v", code, got, want)
		}
	}
}
