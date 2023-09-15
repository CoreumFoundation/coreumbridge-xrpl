package logger

import (
	"github.com/samber/lo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var _ Logger = &ZapLogger{}

// ZapLogger is logger wrapper with an ability to add error logs metric record.
type ZapLogger struct {
	zapLogger *zap.Logger
}

// NewZapLogger returns a new instance of the ZapLogger.
func NewZapLogger(zapLogger *zap.Logger) *ZapLogger {
	return &ZapLogger{
		zapLogger: zapLogger,
	}
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
