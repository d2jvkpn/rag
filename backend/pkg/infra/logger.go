package infra

import (
	"os"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// L is initialized to a no-op logger so tests that skip Init() don't panic.
var L = zap.NewNop()

// consoleCore/fileCore are retained after Init so EnableFileLogging can
// upgrade L from console-only to console+file without rebuilding encoders
// from config again.
var (
	consoleCore zapcore.Core
	fileCore    zapcore.Core
)

// Init builds a console-only logger and assigns it to L. Use this during
// startup/wiring, before the service is ready to serve, so that no
// pre-serving log lines (including Fatal exits during init) reach the log
// file. Call EnableFileLogging once the service is about to start serving.
func Init(config *viper.Viper) {
	var (
		level      zapcore.Level
		encoderCfg zapcore.EncoderConfig
		fileWriter zapcore.WriteSyncer
	)

	level = zapcore.DebugLevel
	if config.GetBool("app.release") {
		level = zapcore.InfoLevel
	}

	encoderCfg = zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "ts"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	fileWriter = zapcore.AddSync(&lumberjack.Logger{
		Filename:   config.GetString("logging.path"),
		MaxSize:    config.GetInt("logging.max_size_mb"), // MB
		MaxBackups: config.GetInt("logging.max_backups"),
		MaxAge:     config.GetInt("logging.max_age_days"), // days
		Compress:   config.GetBool("logging.compress"),
	})

	consoleCore = zapcore.NewCore(zapcore.NewConsoleEncoder(encoderCfg), zapcore.AddSync(os.Stdout), level)
	fileCore = zapcore.NewCore(zapcore.NewJSONEncoder(encoderCfg), fileWriter, level)

	L = zap.New(consoleCore, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

// EnableFileLogging upgrades L to also write to the rotated log file, on top
// of the console. Call this once, immediately before the service starts
// serving.
func EnableFileLogging() {
	if consoleCore == nil || fileCore == nil {
		return
	}
	L = zap.New(zapcore.NewTee(consoleCore, fileCore), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

func Sync() {
	if L != nil {
		_ = L.Sync()
	}
}
