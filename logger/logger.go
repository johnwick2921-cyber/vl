package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	// Log is the global logger instance
	Log *logrus.Logger
	// logFile holds the current log file handle
	logFile *os.File

	ctLocOnce sync.Once
	ctLoc     *time.Location
)

// CTLocation returns the cached America/Chicago location for log rendering.
// logger cannot import kernel (kernel imports logger — an import cycle), so it
// owns its own copy of the canonical-zone load with the SAME fail-closed
// fallback kernel.CTLocation uses: UTC only when the zone database is missing,
// never local time. Every log line timestamp and every log-file date routes
// through this one location.
func CTLocation() *time.Location {
	ctLocOnce.Do(func() {
		loc, err := time.LoadLocation("America/Chicago")
		if err != nil {
			loc = time.UTC
		}
		ctLoc = loc
	})
	return ctLoc
}

// CTDate renders t as "2006-01-02" in the canonical America/Chicago zone. It is
// the single date source for the log-file name: the writer (LogFileNameFor) and
// the updater's predictor (internal/updaterworker.predictedLog) both call it so
// the file the bot writes and the file the updater reads are the same CT date
// on any machine zone.
func CTDate(t time.Time) string {
	return t.In(CTLocation()).Format("2006-01-02")
}

// compactFormatter is a custom formatter for cleaner log output
type compactFormatter struct {
	logrus.TextFormatter
}

func (f *compactFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	level := strings.ToUpper(entry.Level.String())[0:4]
	timestamp := entry.Time.In(CTLocation()).Format("01-02 15:04:05")

	// Skip frames to find actual caller (skip logrus + our wrapper functions)
	caller := ""
	for i := 3; i < 10; i++ {
		_, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		// Skip logrus internal and our logger.go
		if !strings.Contains(file, "logrus") && !strings.HasSuffix(file, "logger/logger.go") {
			// Get package name from path (e.g., "vl/manager/trader_manager.go" -> "manager")
			dir := filepath.Dir(file)
			pkg := filepath.Base(dir)
			caller = fmt.Sprintf("%s/%s:%d", pkg, filepath.Base(file), line)
			break
		}
	}

	msg := fmt.Sprintf("%s [%s] %s %s\n", timestamp, level, caller, entry.Message)
	return []byte(msg), nil
}

func init() {
	// Auto-initialize default logger to ensure it works before Init is called
	Log = logrus.New()
	Log.SetLevel(logrus.InfoLevel)
	Log.SetFormatter(&compactFormatter{})
	Log.SetOutput(os.Stdout)
}

// ============================================================================
// Initialization functions
// ============================================================================

// LogFileNameFor builds the bot's log file name for the given instant's
// America/Chicago date: <dir>/vl_<YYYY-MM-DD>.log. The date is CT, not the host
// zone, so the file a boot writes and the line times inside it agree on any
// machine clock. The updater's boot-line prediction (logPrefixForBinary in
// internal/updaterworker) must agree with the prefix this function writes — a
// parity test pins the pair.
func LogFileNameFor(dir string, t time.Time) string {
	return filepath.Join(dir, fmt.Sprintf("vl_%s.log", CTDate(t)))
}

// Init initializes the global logger
// If config is nil, uses default configuration (console output, info level)
func Init(cfg *Config) error {
	Log = logrus.New()

	// Use default values if no config provided
	if cfg == nil {
		cfg = &Config{Level: "info"}
	}

	// Set default values
	cfg.SetDefaults()

	// Set log level
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	Log.SetLevel(level)

	// Set compact formatter
	Log.SetFormatter(&compactFormatter{})

	// Setup log file output (write to both stdout and file)
	logDir := "data"
	if err := os.MkdirAll(logDir, 0755); err == nil {
		logFileName := LogFileNameFor(logDir, time.Now())
		f, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			logFile = f
			// Write to both stdout and file
			Log.SetOutput(io.MultiWriter(os.Stdout, f))
		} else {
			Log.SetOutput(os.Stdout)
		}
	} else {
		Log.SetOutput(os.Stdout)
	}

	Log.SetReportCaller(true)

	// P2-2 — age-based prune of data/vl_*.log behind LOG_RETENTION_DAYS
	// (default OFF). Never deletes today's or the running boot's file.
	pruneLogFilesAtInit()

	return nil
}

// InitWithSimpleConfig initializes logger with simplified config
// Suitable for scenarios that only need basic functionality
func InitWithSimpleConfig(level string) error {
	return Init(&Config{Level: level})
}

// Shutdown gracefully shuts down the logger
func Shutdown() {
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}

// ============================================================================
// Logging functions
// ============================================================================

// WithFields creates logger entry with fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return Log.WithFields(fields)
}

// WithField creates logger entry with a single field
func WithField(key string, value interface{}) *logrus.Entry {
	return Log.WithField(key, value)
}

// add debug, info, warn
func Debug(args ...interface{}) {
	Log.Debug(args...)
}

func Info(args ...interface{}) {
	Log.Info(args...)
}

func Warn(args ...interface{}) {
	Log.Warn(args...)
}

func Debugf(format string, args ...interface{}) {
	Log.Debugf(format, args...)
}

func Infof(format string, args ...interface{}) {
	Log.Infof(format, args...)
}

func Warnf(format string, args ...interface{}) {
	Log.Warnf(format, args...)
}

func Error(args ...interface{}) {
	Log.Error(args...)
}

func Errorf(format string, args ...interface{}) {
	Log.Errorf(format, args...)
}

func Fatal(args ...interface{}) {
	Log.Fatal(args...)
}

func Fatalf(format string, args ...interface{}) {
	Log.Fatalf(format, args...)
}

func Panic(args ...interface{}) {
	Log.Panic(args...)
}

func Panicf(format string, args ...interface{}) {
	Log.Panicf(format, args...)
}

// ============================================================================
// MCP Logger adapter
// ============================================================================

// MCPLogger adapter that allows MCP package to use the global logger
// Implements mcp.Logger interface
type MCPLogger struct{}

// NewMCPLogger creates MCP log adapter
func NewMCPLogger() *MCPLogger {
	return &MCPLogger{}
}

func (l *MCPLogger) Debugf(format string, args ...any) {
	Log.Debugf(format, args...)
}

func (l *MCPLogger) Infof(format string, args ...any) {
	Log.Infof(format, args...)
}

func (l *MCPLogger) Warnf(format string, args ...any) {
	Log.Warnf(format, args...)
}

func (l *MCPLogger) Errorf(format string, args ...any) {
	Log.Errorf(format, args...)
}
