package logger

import (
	"context"

	"go.uber.org/zap"
)

//go:generate mockgen -destination=mock.go -package=logger . Logger

// A Field is a marshaling operation used to add a key-value pair to a logger's context. Most fields are lazily
// marshaled, so it's inexpensive to add fields to disabled debug-level log statements.
type Field struct {
	Key       string
	Type      FieldType
	Integer   int64
	String    string
	Interface any
}

// A FieldType indicates which member of the Field union struct should be used and how it should be serialized.
type FieldType uint8

// Logger is a logger interface.
type Logger interface {
	Debug(ctx context.Context, msg string, fields ...Field)
	Info(ctx context.Context, msg string, fields ...Field)
	Warn(ctx context.Context, msg string, fields ...Field)
	Error(ctx context.Context, msg string, fields ...Field)
}

// AnyField takes a key and an arbitrary value and chooses the best way to represent them as a field, falling back to a
// reflection-based approach only if necessary.
func AnyField(key string, value any) Field {
	return convertZapFieldToField(zap.Any(key, value))
}

// StringField constructs a field with the given key and value.
func StringField(key, value string) Field {
	return convertZapFieldToField(zap.String(key, value))
}

// Uint32Field constructs a field with the given key and value.
func Uint32Field(key string, value uint32) Field {
	return convertZapFieldToField(zap.Uint32(key, value))
}

// Int64Field constructs a field with the given key and value.
func Int64Field(key string, value int64) Field {
	return convertZapFieldToField(zap.Int64(key, value))
}

// Uint64Field constructs a field with the given key and value.
func Uint64Field(key string, value uint64) Field {
	return convertZapFieldToField(zap.Uint64(key, value))
}

// Error is shorthand for the common idiom NamedError("error", err).
func Error(err error) Field {
	return convertZapFieldToField(zap.Error(err))
}

func convertZapFieldToField(zapField zap.Field) Field {
	return Field{
		Key:       zapField.Key,
		Type:      FieldType(zapField.Type),
		Integer:   zapField.Integer,
		String:    zapField.String,
		Interface: zapField.Interface,
	}
}
