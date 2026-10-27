package conformance_test

import "log/slog"

func slogDiscard() *slog.Logger { return slog.New(slog.DiscardHandler) }
