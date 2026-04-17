package logger

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/lumberjack.v2"
)

// L is the global application logger. Call Init() before use.
var L *zap.Logger

// Init creates the global logger.
//
//   - dev=true  → coloured console encoder
//   - dev=false → JSON console encoder
//
// Logs are always also written to a rotating file (JSON) at LOG_FILE
// (default: logs/app.log). Log level is read from LOG_LEVEL env var
// (debug/info/warn/error); defaults to debug.
func Init(dev bool) {
	level := zapcore.DebugLevel
	if lvl, ok := os.LookupEnv("LOG_LEVEL"); ok {
		if err := level.UnmarshalText([]byte(lvl)); err != nil {
			level = zapcore.DebugLevel
		}
	}

	logFile := os.Getenv("LOG_FILE")
	if logFile == "" {
		logFile = "logs/app.log"
	}

	// ── Console sink ──────────────────────────────────────────────────────────
	var consoleEnc zapcore.Encoder
	if dev {
		encCfg := zap.NewDevelopmentEncoderConfig()
		encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encCfg.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000")
		consoleEnc = zapcore.NewConsoleEncoder(encCfg)
	} else {
		encCfg := zap.NewProductionEncoderConfig()
		encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		consoleEnc = zapcore.NewJSONEncoder(encCfg)
	}
	consoleSink := zapcore.AddSync(os.Stdout)

	// ── File sink (always JSON, rotating) ────────────────────────────────────
	if err := os.MkdirAll(filepath.Dir(logFile), 0o750); err != nil {
		panic("logger: cannot create log dir: " + err.Error())
	}

	fileEncCfg := zap.NewProductionEncoderConfig()
	fileEncCfg.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000")
	fileEnc := zapcore.NewJSONEncoder(fileEncCfg)

	fileSink := zapcore.AddSync(&lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    100,  // MB per file before rotation
		MaxBackups: 7,    // number of rotated files to keep
		MaxAge:     30,   // days
		Compress:   true, // gzip rotated files
	})

	// ── Tee: write to both console and file ───────────────────────────────────
	core := zapcore.NewTee(
		zapcore.NewCore(consoleEnc, consoleSink, level),
		zapcore.NewCore(fileEnc, fileSink, level),
	)

	L = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

// Sync flushes any buffered log entries. Call defer logger.Sync() in main.
func Sync() { _ = L.Sync() }
