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

func Init(config *viper.Viper) {
	fileWriter := zapcore.AddSync(&lumberjack.Logger{
		Filename:   config.GetString("logging.path"),
		MaxSize:    config.GetInt("logging.max_size_mb"), // MB
		MaxBackups: config.GetInt("logging.max_backups"),
		MaxAge:     config.GetInt("logging.max_age_days"), // days
		Compress:   config.GetBool("logging.compress"),
	})

	level := zapcore.DebugLevel
	if config.GetBool("app.release") {
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
