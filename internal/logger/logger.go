package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// L is the global application logger. Call Init() before use.
var L *zap.Logger

// Init creates the global logger. dev=true → coloured console; dev=false → JSON.
// Log level is read from the LOG_LEVEL env var (debug/info/warn/error); defaults to debug.
func Init(dev bool) {
	level := zapcore.DebugLevel
	if lvl, ok := os.LookupEnv("LOG_LEVEL"); ok {
		if err := level.UnmarshalText([]byte(lvl)); err != nil {
			level = zapcore.DebugLevel
		}
	}

	var enc zapcore.Encoder
	encCfg := zap.NewDevelopmentEncoderConfig()
	encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	encCfg.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000")

	if dev {
		enc = zapcore.NewConsoleEncoder(encCfg)
	} else {
		prodCfg := zap.NewProductionEncoderConfig()
		prodCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		enc = zapcore.NewJSONEncoder(prodCfg)
	}

	core := zapcore.NewCore(enc, zapcore.AddSync(os.Stdout), level)
	L = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

// Sync flushes any buffered log entries. Call defer logger.Sync() in main.
func Sync() { _ = L.Sync() }
