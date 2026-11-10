package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// ANSI SGR codes.
const (
	bold       = "1"
	red        = "31"
	green      = "32"
	yellow     = "33"
	cyan       = "36"
	gray       = "90"
	brightBlue = "94"
	brightCyan = "96"
)

// styler colors text with ANSI codes when enabled.
type styler bool

func (s styler) style(text string, codes ...string) string {
	if !s || len(codes) == 0 {
		return text
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

// logger writes leveled diagnostics to stderr. It is safe for concurrent use.
type logger struct {
	mu    sync.Mutex
	w     io.Writer
	level logLevel
	color styler
}

func (l *logger) logf(level logLevel, format string, args ...any) {
	if level < l.level {
		return
	}
	var label string
	switch level {
	case levelDebug:
		label = l.color.style("DEBUG", gray)
	case levelInfo:
		label = l.color.style("INFO", cyan)
	case levelWarn:
		label = l.color.style("WARNING", yellow)
	default:
		label = l.color.style("ERROR", red)
	}
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s %s\n", label, msg)
}

func (l *logger) Debugf(format string, args ...any) { l.logf(levelDebug, format, args...) }
func (l *logger) Infof(format string, args ...any)  { l.logf(levelInfo, format, args...) }
func (l *logger) Warnf(format string, args ...any)  { l.logf(levelWarn, format, args...) }
