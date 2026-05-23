package logger

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// L is initialized to a no-op logger so tests that skip Init() don't panic.
var L = zap.NewNop()

func Init(logDir string, release bool) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		panic("logger: cannot create log dir: " + err.Error())
	}

	fileWriter := zapcore.AddSync(&lumberjack.Logger{
		Filename:   filepath.Join(logDir, "app.log"),
		MaxSize:    100, // MB
		MaxBackups: 7,
		MaxAge:     30, // days
		Compress:   true,
	})

	level := zapcore.DebugLevel
	if release {
		level = zapcore.InfoLevel
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "ts"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	consoleEncoder := zapcore.NewConsoleEncoder(encoderCfg)
	fileEncoder := zapcore.NewJSONEncoder(encoderCfg)

	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), level),
		zapcore.NewCore(fileEncoder, fileWriter, level),
	)

	L = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

func Sync() {
	if L != nil {
		_ = L.Sync()
	}
}
