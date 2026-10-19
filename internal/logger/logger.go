// Package logger собирает zap-логгер по конфигу: уровень, формат
// (json / console) и опциональную запись в файл с ротацией по дате
// и размеру. Пишет всегда в stdout, файл — по желанию.
package logger

import (
	"fmt"
	"os"

	"github.com/dmitriy-mvdness/hw-checker/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New собирает zap-логгер по конфигу: уровень, формат, и — опционально —
// запись в файл с ротацией по дате.
//
// Пишем всегда в stdout. Если Enabled и Dir заданы — дублируем в файл.
// Ошибка файлового writer'а не роняет приложение: пишем warning в stderr
// и работаем только с консолью.
func New(cfg config.LoggerConfig) (*zap.Logger, error) {
	var encoder zapcore.Encoder
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.TimeKey = "timestamp"

	opts := []zap.Option{zap.AddCaller()}

	if cfg.Environment == "development" {
		encoderCfg = zap.NewDevelopmentEncoderConfig()
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
		opts = append(opts, zap.Development(), zap.AddStacktrace(zapcore.WarnLevel))
	} else {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
		opts = append(opts, zap.AddStacktrace(zapcore.ErrorLevel))
	}

	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(cfg.Level)); err != nil {
		lvl = zapcore.InfoLevel
	}
	levelEnabler := zap.NewAtomicLevelAt(lvl)

	// Всегда пишем в stdout.
	syncers := []zapcore.WriteSyncer{zapcore.AddSync(os.Stdout)}

	// Файл — по желанию.
	if cfg.Enabled && cfg.Dir != "" {
		writer, err := NewDailyWriter(
			cfg.Dir,
			int64(cfg.MaxSizeMB),
			cfg.MaxFiles,
			cfg.MaxAgeDays,
			cfg.Compress,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "лог в файл отключён: %v\n", err)
		} else {
			syncers = append(syncers, zapcore.AddSync(writer))
		}
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.NewMultiWriteSyncer(syncers...),
		levelEnabler,
	)

	return zap.New(core, opts...), nil
}
