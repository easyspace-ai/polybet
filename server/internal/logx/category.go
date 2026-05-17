package logx

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

const (
	FileTrade      = "trade.log"
	FilePosition   = "position.log"
	FileOpen       = "open.log"
	FileStopLoss   = "stoploss.log"
	FileHTTPAccess = "http-access.log"
)

var (
	categoryMu     sync.Mutex
	tradeLogger    *logrus.Logger
	positionLogger *logrus.Logger
	openLogger     *logrus.Logger
	stopLossLogger *logrus.Logger
	categoryFiles  []*os.File
	discardLogger  *logrus.Logger
)

func init() {
	discardLogger = logrus.New()
	discardLogger.SetOutput(io.Discard)
	discardLogger.SetLevel(logrus.PanicLevel)
}

func fileTextFormatter() logrus.Formatter {
	return &logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05.000000000Z07:00",
		DisableColors:   true,
	}
}

func newCategoryLogger(f *os.File) *logrus.Logger {
	l := logrus.New()
	l.SetOutput(f)
	l.SetFormatter(fileTextFormatter())
	l.SetLevel(logrus.DebugLevel)
	return l
}

func openCategoryFile(dir, name string) (*os.File, error) {
	path := filepath.Join(dir, name)
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func defaultLogsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".polybet", "logs")
	if d := strings.TrimSpace(os.Getenv("POLYBET_LOG_DIR")); d != "" {
		dir = d
	}
	return dir, nil
}

// OpenCategoryLoggers opens append-only category log files under PolybetLogsDir().
// When disk logging is disabled or the directory is unavailable, category loggers
// discard output. Safe to call multiple times; reopens files on repeat calls.
func OpenCategoryLoggers() error {
	if strings.TrimSpace(os.Getenv("POLYBET_DISABLE_DISK_LOG")) == "1" {
		return nil
	}
	dir := PolybetLogsDir()
	if dir == "" {
		var err error
		dir, err = defaultLogsDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	type spec struct {
		name string
		dest **logrus.Logger
	}
	specs := []spec{
		{FileTrade, &tradeLogger},
		{FilePosition, &positionLogger},
		{FileOpen, &openLogger},
		{FileStopLoss, &stopLossLogger},
	}

	categoryMu.Lock()
	defer categoryMu.Unlock()

	for _, f := range categoryFiles {
		_ = f.Close()
	}
	categoryFiles = categoryFiles[:0]
	tradeLogger = nil
	positionLogger = nil
	openLogger = nil
	stopLossLogger = nil

	for _, s := range specs {
		f, err := openCategoryFile(dir, s.name)
		if err != nil {
			closeCategoryFilesLocked()
			return err
		}
		categoryFiles = append(categoryFiles, f)
		*s.dest = newCategoryLogger(f)
	}
	return nil
}

func closeCategoryFilesLocked() {
	for _, f := range categoryFiles {
		_ = f.Close()
	}
	categoryFiles = categoryFiles[:0]
	tradeLogger = nil
	positionLogger = nil
	openLogger = nil
	stopLossLogger = nil
}

// CloseCategoryLoggers closes category log files. Safe to call multiple times.
func CloseCategoryLoggers() {
	categoryMu.Lock()
	defer categoryMu.Unlock()
	closeCategoryFilesLocked()
}

func categoryOrDiscard(l *logrus.Logger) *logrus.Logger {
	if l != nil {
		return l
	}
	return discardLogger
}

// Trade returns a logger that writes trade/CLOB events to trade.log only.
func Trade() *logrus.Logger {
	categoryMu.Lock()
	defer categoryMu.Unlock()
	return categoryOrDiscard(tradeLogger)
}

// Position returns a logger for position list/sync/update events.
func Position() *logrus.Logger {
	categoryMu.Lock()
	defer categoryMu.Unlock()
	return categoryOrDiscard(positionLogger)
}

// Open returns a logger for new exposure / open-position events.
func Open() *logrus.Logger {
	categoryMu.Lock()
	defer categoryMu.Unlock()
	return categoryOrDiscard(openLogger)
}

// StopLoss returns a logger for stop-loss evaluation and close tasks.
func StopLoss() *logrus.Logger {
	categoryMu.Lock()
	defer categoryMu.Unlock()
	return categoryOrDiscard(stopLossLogger)
}
