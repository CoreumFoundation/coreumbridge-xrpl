package logger

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/tracing"
)

var _ Logger = &ZapLogger{}

const (
	tracingXRPLTxHashFieldName = "xrplTxHash"
	tracingIDFieldName         = "tracingID"
	tracingProcessFieldName    = "process"
)

var _ ParallelLogger = &ParallelZapLogger{}

// ParallelZapLogger is parallel zap logger.
type ParallelZapLogger struct {
	ctx    context.Context //nolint:containedctx // the design depends on the parallel logger design where the ctx is set similar
	zapLog *ZapLogger
}

// NewParallelZapLogger return new instance of the ParallelZapLogger.
func NewParallelZapLogger(ctx context.Context, zapLog *ZapLogger) *ParallelZapLogger {
	return &ParallelZapLogger{
		ctx:    ctx,
		zapLog: zapLog,
	}
}

// Debug prints debug log.
func (p *ParallelZapLogger) Debug(name string, id int64, onExit parallel.OnExit, message string) {
	p.zapLog.Named(name).Debug(
		p.ctx, message,
		Int64Field("id", id),
		StringField("onExit", onExit.String()),
	)
}

// Error prints error log.
func (p *ParallelZapLogger) Error(name string, id int64, onExit parallel.OnExit, message string, err error) {
	// the context canceled is not an error
	if errors.Is(err, context.Canceled) {
		return
	}
	var panicErr parallel.ErrPanic
	if errors.As(err, &panicErr) {
		p.zapLog.Named(name).Error(
			p.ctx,
			message,
			Int64Field("id", id),
			StringField("onExit", onExit.String()),
			StringField("value", fmt.Sprint(panicErr.Value)),
			ByteStringField("stack", panicErr.Stack),
			Error(err),
		)
		return
	}
	p.zapLog.Named(name).Error(
		p.ctx,
		message,
		Int64Field("id", id),
		StringField("onExit", onExit.String()),
		Error(err),
	)
}

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
func (z *ZapLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	z.zapLogger.Debug(msg, filedToZapField(ctx, fields...)...)
}

// Info logs a message at InfoLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z *ZapLogger) Info(ctx context.Context, msg string, fields ...Field) {
	z.zapLogger.Info(msg, filedToZapField(ctx, fields...)...)
}

// Warn logs a message at WarnLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z *ZapLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	z.zapLogger.Warn(msg, filedToZapField(ctx, fields...)...)
}

// Error logs a message at ErrorLevel. The message includes any fields passed at the log site, as well as any fields
// accumulated on the logger.
func (z *ZapLogger) Error(ctx context.Context, msg string, fields ...Field) {
	z.zapLogger.Error(msg, filedToZapField(ctx, fields...)...)
}

// Named adds a new path segment to the logger's name. Segments are joined by
// periods. By default, Loggers are unnamed.
func (z *ZapLogger) Named(name string) *ZapLogger {
	return NewZapLoggerFromLogger(z.zapLogger.Named(name))
}

// ParallelLogger returns parallel zap logger.
func (z *ZapLogger) ParallelLogger(ctx context.Context) ParallelLogger {
	return NewParallelZapLogger(ctx, z)
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
