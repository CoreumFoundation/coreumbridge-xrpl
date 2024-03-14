package logger

import (
	"context"
	"strings"

	"github.com/pkg/errors"
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
	// CallerSkip increases the number of callers skipped by caller annotation
	// (as enabled by the AddCaller option). When building wrappers around the
	// Logger and SugaredLogger, supplying this Option prevents zap from always
	// reporting the wrapper code as the caller.
	CallerSkip int
}

// DefaultZapLoggerConfig returns default ZapLoggerConfig.
func DefaultZapLoggerConfig() ZapLoggerConfig {
	return ZapLoggerConfig{
		Level:  "info",
		Format: "console",
		//
		CallerSkip: 2,
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

	zapLogger, err := zapCfg.Build(
		zap.AddCaller(),
		zap.AddCallerSkip(cfg.CallerSkip),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build zap logger from the config, config:%+v", zapCfg)
	}

	return &ZapLogger{
		zapLogger: zapLogger,
	}, nil
}

// Debug logs a message at DebugLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z *ZapLogger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	z.zapLogger.Debug(msg, addContextFields(ctx, fields...)...)
}

// Info logs a message at InfoLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z *ZapLogger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	z.zapLogger.Info(msg, addContextFields(ctx, fields...)...)
}

// Warn logs a message at WarnLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z *ZapLogger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	z.zapLogger.Warn(msg, addContextFields(ctx, fields...)...)
}

// Error logs a message at ErrorLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z *ZapLogger) Error(ctx context.Context, msg string, fields ...zap.Field) {
	z.zapLogger.Error(msg, addContextFields(ctx, fields...)...)
}

func addContextFields(ctx context.Context, fields ...zap.Field) []zap.Field {
	// add tracing info from the context
	xrplTxHash := tracing.GetTracingXRPLTxHash(ctx)
	if xrplTxHash != "" {
		fields = append(fields, zap.String(tracingXRPLTxHashFieldName, xrplTxHash))
	}
	tracingID := tracing.GetTracingID(ctx)
	if tracingID != "" {
		fields = append(fields, zap.String(tracingIDFieldName, tracingID))
	}
	processID := tracing.GetTracingProcess(ctx)
	if processID != "" {
		fields = append(fields, zap.String(tracingProcessFieldName, processID))
	}

	return fields
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
