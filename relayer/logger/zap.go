package logger

import (
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var _ Logger = &ZapLogger{}

// ZapLoggerConfig is ZapLogger config.
type ZapLoggerConfig struct {
	Level  string
	Format string
}

// DefaultZapLoggerConfig returns default ZapLoggerConfig.
func DefaultZapLoggerConfig() ZapLoggerConfig {
	return ZapLoggerConfig{
		Level:  "info",
		Format: "console",
	}
}

// ZapLogger is logger wrapper with an ability to add error logs metric record.
type ZapLogger struct {
	zapLogger *zap.Logger
}

// NewZapLoggerFromLogger returns a new instance of the ZapLogger.
func NewZapLoggerFromLogger(zapLogger *zap.Logger) *ZapLogger {
	return &ZapLogger{
		zapLogger: zapLogger,
	}
}

// NewZapLogger creates a new instance of the zap.Logger with .
func NewZapLogger(cfg ZapLoggerConfig) (*ZapLogger, error) {
	logLevel, err := stringToLoggerLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	zapCfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(logLevel),
		Development:      false,
		Encoding:         cfg.Format,
		EncoderConfig:    encoderConfig,
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}

	zapLogger, err := zapCfg.Build(zap.AddCaller(), zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build zap logger form the config, config:%+v", zapCfg)
	}

	return &ZapLogger{
		zapLogger: zapLogger,
	}, nil
}

// Debug logs a message at DebugLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z ZapLogger) Debug(msg string, fields ...Field) {
	z.zapLogger.Debug(msg, filedToZapFiled(fields...)...)
}

// Info logs a message at InfoLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z ZapLogger) Info(msg string, fields ...Field) {
	z.zapLogger.Info(msg, filedToZapFiled(fields...)...)
}

// Warn logs a message at WarnLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z ZapLogger) Warn(msg string, fields ...Field) {
	z.zapLogger.Warn(msg, filedToZapFiled(fields...)...)
}

// Error logs a message at ErrorLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z ZapLogger) Error(msg string, fields ...Field) {
	z.zapLogger.Error(msg, filedToZapFiled(fields...)...)
}

func filedToZapFiled(fields ...Field) []zap.Field {
	return lo.Map(fields, func(filed Field, _ int) zap.Field {
		return zap.Field{
			Key:       filed.Key,
			Type:      zapcore.FieldType(filed.Type),
			Integer:   filed.Integer,
			String:    filed.String,
			Interface: filed.Interface,
		}
	})
}

// stringToLoggerLevel converts the string level to zapcore.Level.
func stringToLoggerLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return 0, errors.Errorf("unknown log level: %q", level)
	}
}
