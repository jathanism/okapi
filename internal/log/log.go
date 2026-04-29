package log

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

// Our custom log levels
var (
	TraceLevel  = log.DebugLevel - 4
	DebugLevel  = log.DebugLevel
	InfoLevel   = log.InfoLevel
	WarnLevel   = log.WarnLevel
	ErrorLevel  = log.ErrorLevel
	FatalLevel  = log.FatalLevel
	NoLogsLevel = log.Level(50)
)

// nullLogger is a function that outputs nothing.
var nullLogger = func(msg any, keyVals ...any) {}

// defaultLogger is our logger instance for non-Trace level logs
var defaultLogger *log.Logger

// This is our logger instance with Trace enabled
var traceLogger *log.Logger

// Public function bindings for the default logger
var (
	Trace, Debug, Info, Warn, Error, Fatal func(msg any, keyVals ...any)
)

// Prevent race conditions in DetectTrace.
var detectMux = &sync.Mutex{}

func init() {
	Init(os.Stdout)
}

// Create creates a new logger instance.
func Create(output io.Writer) (regular *log.Logger, trace *log.Logger) {
	// Create a new logger with trace logs enabled
	regular = log.NewWithOptions(output, log.Options{
		Level:           ErrorLevel,
		ReportTimestamp: true,
		ReportCaller:    DetectTraceEnv(),
	})

	// Create a new logger with trace logs enabled
	trace = log.NewWithOptions(output, log.Options{
		Level:           ErrorLevel,
		ReportTimestamp: true,
		ReportCaller:    DetectTraceEnv(),
	})
	// Set custom styles so debug looks like trace
	styles := log.DefaultStyles()
	styles.Levels[DebugLevel] = lipgloss.NewStyle().
		SetString("TRACE").
		Bold(true).
		Foreground(lipgloss.Color("#aaaa00"))
	trace.SetStyles(styles)

	return
}

// Initialize the traceLogger instance.
func Init(output io.Writer) {
	// Create a new logger with trace logs enabled
	defaultLogger, traceLogger = Create(output)
	Trace = traceLogger.Debug
	Debug = defaultLogger.Debug
	Info = defaultLogger.Info
	Warn = defaultLogger.Warn
	Error = defaultLogger.Error
	Fatal = defaultLogger.Fatal

	DetectTrace()
}

// Swap swaps out the logger output target for a new one and returns a function
// to restore the original.
func Swap(output io.Writer) func() {
	origDefaultLogger := defaultLogger
	origTraceLogger := traceLogger

	Init(output)

	// Preserve previous level
	defaultLogger.SetLevel(origDefaultLogger.GetLevel())
	traceLogger.SetLevel(origDefaultLogger.GetLevel())

	// Preserve programmatic trace
	DetectTrace()

	return func() {
		traceLogger = origTraceLogger
		defaultLogger = origDefaultLogger
	}
}

// DetectTrace detects if the DEBUG environment variable is set to trace and
// enables trace logs.
func DetectTrace() bool {
	detectMux.Lock()
	defer detectMux.Unlock()
	if DetectTraceEnv() || defaultLogger.GetLevel() == TraceLevel {
		ToggleTrace(true)
		return true
	} else {
		ToggleTrace(false)
	}
	return false
}

// DetectTraceEnv detects if the DEBUG environment variable is set to trace.
func DetectTraceEnv() bool {
	return strings.ToLower(os.Getenv("DEBUG")) == "trace"
}

// ToggleTrace toggles the trace logs.
func ToggleTrace(toggle bool) {
	if toggle {
		Trace = traceLogger.Debug
	} else {
		Trace = nullLogger
	}
}

// SetLevel sets the log level for the logger.
// If the requested level is TraceLevel, it will enable trace logs and set the
// level to TraceLevel.
func SetLevel(level log.Level) {
	ToggleTrace(level == TraceLevel)
	defaultLogger.SetLevel(level)
	traceLogger.SetLevel(level)
}

// DetectLevel detects the appropriate level based on the DEBUG environment
// variable's contents.
func DetectLevel() {
	if DetectTraceEnv() {
		SetLevel(TraceLevel)
	} else if os.Getenv("DEBUG") != "" {
		SetLevel(DebugLevel)
	} else {
		SetLevel(ErrorLevel)
	}
}

// GetLevel returns the level of the default logger.
func GetLevel() log.Level {
	return defaultLogger.GetLevel()
}
