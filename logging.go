package cascadingconfig

import (
	"context"
	"log/slog"
	"time"
)

// levelOf is the level a warning with code is logged at (DL-263): Info for expected behaviour worth knowing, Warn for
// everything else, including codes from other modules.
func levelOf(code WarningCode) slog.Level {
	switch code {
	case WarnUntrusted, WarnFormatConflict, WarnNoHome:
		return slog.LevelInfo
	}
	return slog.LevelWarn
}

// log returns the logger the loader logs to: the one WithLogger set, or slog.Default() as it is now, with records
// below WithLogLevel's level dropped first.
func (c *config) log() *slog.Logger {
	l := c.logger
	if l == nil {
		l = slog.Default()
	}
	if c.logLevel == nil {
		return l
	}
	return slog.New(levelFilter{h: l.Handler(), level: c.logLevel})
}

// levelFilter is a handler that drops records below level before passing them to h.
type levelFilter struct {
	h     slog.Handler
	level slog.Leveler
}

func (f levelFilter) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= f.level.Level() && f.h.Enabled(ctx, l)
}

func (f levelFilter) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < f.level.Level() {
		return nil
	}
	return f.h.Handle(ctx, r)
}

func (f levelFilter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelFilter{h: f.h.WithAttrs(attrs), level: f.level}
}

func (f levelFilter) WithGroup(name string) slog.Handler {
	return levelFilter{h: f.h.WithGroup(name), level: f.level}
}

// trace logs a finished load at Debug: each layer, each remote include's cache outcome, and a summary.
func (ld *loading) trace(rep *Report, took time.Duration) {
	logger := ld.c.log()
	if !logger.Enabled(ld.ctx, slog.LevelDebug) {
		return
	}
	loaded, skipped := 0, 0
	for _, l := range rep.Layers {
		if l.Status == LayerLoaded {
			loaded++
		} else {
			skipped++
		}
		attrs := []slog.Attr{slog.String("op", "layer"), slog.String("status", l.Status.String()),
			slog.String("scope", string(l.Scope))}
		if l.Path != "" {
			attrs = append(attrs, slog.String("path", l.Path))
		}
		logger.LogAttrs(ld.ctx, slog.LevelDebug, "layer "+l.Status.String(), attrs...)
		if l.Remote != nil {
			outcome := string(l.Remote.Cache)
			if l.Remote.Cache == CacheFetched {
				outcome = "fetched"
			}
			logger.LogAttrs(ld.ctx, slog.LevelDebug, "remote include "+outcome, slog.String("op", "remote"),
				slog.String("ref", l.Remote.Ref), slog.String("cache", outcome))
		}
	}
	logger.LogAttrs(ld.ctx, slog.LevelDebug, "load finished", slog.String("op", "load"),
		slog.Int("loaded", loaded), slog.Int("skipped", skipped), slog.Duration("duration", took))
}
