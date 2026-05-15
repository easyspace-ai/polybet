// Package logx configures process-wide logrus output for the server binary.
package logx

import (
	"os"

	"github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
)

// Configure sets global logrus formatter (text, full timestamp, TTY colors) and level from LOG_LEVEL-style strings.
func Configure(level string) {
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(ParseLevel(level))
	forceColors := isatty.IsTerminal(os.Stdout.Fd())
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05.000000000Z07:00",
		ForceColors:     forceColors,
		DisableColors:   !forceColors,
	})
}

// ParseLevel maps LOG_LEVEL env values to logrus levels.
func ParseLevel(s string) logrus.Level {
	switch s {
	case "debug":
		return logrus.DebugLevel
	case "warn":
		return logrus.WarnLevel
	case "error":
		return logrus.ErrorLevel
	default:
		return logrus.InfoLevel
	}
}

// Pairs builds logrus.Fields from alternating key/value arguments (slog-style).
// Non-string keys become "key"; a trailing key without value is ignored.
func Pairs(kv ...any) logrus.Fields {
	if len(kv) == 0 {
		return nil
	}
	n := (len(kv) + 1) / 2
	f := make(logrus.Fields, n)
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			k = "key"
		}
		f[k] = kv[i+1]
	}
	return f
}
