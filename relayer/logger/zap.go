package logger

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/tracing"
)

var _ Logger = &ZapLogger{}

const (
	tracingXRPLTxHashFieldName = "xrplTxHash"
	tracingIDFieldName         = "tracingID"
	tracingProcessFieldName    = "process"
)

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
		return nil, errors.Wrapf(err, "failed to build zap logger from the config, config:%+v", zapCfg)
	}

	return &ZapLogger{
		zapLogger: zapLogger,
	}, nil
}

// Debug logs a message at DebugLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z ZapLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	z.zapLogger.Debug(msg, filedToZapField(ctx, fields...)...)
}

// Info logs a message at InfoLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z ZapLogger) Info(ctx context.Context, msg string, fields ...Field) {
	z.zapLogger.Info(msg, filedToZapField(ctx, fields...)...)
}

// Warn logs a message at WarnLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z ZapLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	z.zapLogger.Warn(msg, filedToZapField(ctx, fields...)...)
}

// Error logs a message at ErrorLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z ZapLogger) Error(ctx context.Context, msg string, fields ...Field) {
	z.zapLogger.Error(msg, filedToZapField(ctx, fields...)...)
}

func filedToZapField(ctx context.Context, fields ...Field) []zap.Field {
	zapFields := lo.Map(fields, func(filed Field, _ int) zap.Field {
		return zap.Field{
			Key:       filed.Key,
			Type:      zapcore.FieldType(filed.Type),
			Integer:   filed.Integer,
			String:    filed.String,
			Interface: filed.Interface,
		}
	})

	// add tracing info from the context
	xrplTxHash := tracing.GetTracingXRPLTxHash(ctx)
	if xrplTxHash != "" {
		zapFields = append(zapFields, zap.String(tracingXRPLTxHashFieldName, xrplTxHash))
	}
	tracingID := tracing.GetTracingID(ctx)
	if tracingID != "" {
		zapFields = append(zapFields, zap.String(tracingIDFieldName, tracingID))
	}
	processID := tracing.GetTracingProcess(ctx)
	if processID != "" {
		zapFields = append(zapFields, zap.String(tracingProcessFieldName, processID))
	}

	return zapFields
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
