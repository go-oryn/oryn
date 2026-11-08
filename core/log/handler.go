package log

import (
	"context"
	"errors"
	"log/slog"
)

var (
	_ slog.Handler = (*LevelHandler)(nil)
	_ slog.Handler = (*FanoutHandler)(nil)
)

// LevelHandler drops the records below a minimum level, then delegates to another handler.
type LevelHandler struct {
	level slog.Leveler
	next  slog.Handler
}

// NewLevelHandler returns a [LevelHandler].
func NewLevelHandler(level slog.Leveler, next slog.Handler) *LevelHandler {
	return &LevelHandler{
		level: level,
		next:  next,
	}
}

// Enabled reports whether level is at least the minimum level, and enabled by the next handler.
func (h *LevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level.Level() && h.next.Enabled(ctx, level)
}

// Handle delegates to the next handler.
func (h *LevelHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.next.Handle(ctx, record)
}

// WithAttrs returns a [LevelHandler] wrapping the next handler with the attributes.
func (h *LevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewLevelHandler(h.level, h.next.WithAttrs(attrs))
}

// WithGroup returns a [LevelHandler] wrapping the next handler with the group.
func (h *LevelHandler) WithGroup(name string) slog.Handler {
	return NewLevelHandler(h.level, h.next.WithGroup(name))
}

// FanoutHandler sends each record to several handlers.
type FanoutHandler struct {
	handlers []slog.Handler
}

// NewFanoutHandler returns a [FanoutHandler].
func NewFanoutHandler(handlers ...slog.Handler) *FanoutHandler {
	return &FanoutHandler{
		handlers: handlers,
	}
}

// Enabled reports whether at least one handler is enabled for level.
func (h *FanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}

	return false
}

// Handle sends a copy of the record to each enabled handler, and joins their errors.
func (h *FanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error

	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			errs = append(errs, handler.Handle(ctx, record.Clone()))
		}
	}

	return errors.Join(errs...)
}

// WithAttrs returns a [FanoutHandler] whose handlers have the attributes.
func (h *FanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithAttrs(attrs))
	}

	return NewFanoutHandler(handlers...)
}

// WithGroup returns a [FanoutHandler] whose handlers have the group.
func (h *FanoutHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithGroup(name))
	}

	return NewFanoutHandler(handlers...)
}
