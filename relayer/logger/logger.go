package logger

import "go.uber.org/zap"

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
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
}

// AnyFiled takes a key and an arbitrary value and chooses the best way to represent them as a field, falling back to a
// reflection-based approach only if necessary.
func AnyFiled(key string, value any) Field {
	return convertZapFieldToField(zap.Any(key, value))
}

// StringFiled constructs a field with the given key and value.
func StringFiled(key, value string) Field {
	return convertZapFieldToField(zap.String(key, value))
}

// Int64Filed constructs a field with the given key and value.
func Int64Filed(key string, value int64) Field {
	return convertZapFieldToField(zap.Int64(key, value))
}

// Uint64Filed constructs a field with the given key and value.
func Uint64Filed(key string, value uint64) Field {
	return convertZapFieldToField(zap.Uint64(key, value))
}

// Error is shorthand for the common idiom NamedError("error", err).
func Error(err error) Field {
	return convertZapFieldToField(zap.Error(err))
}

func convertZapFieldToField(zapFiled zap.Field) Field {
	return Field{
		Key:       zapFiled.Key,
		Type:      FieldType(zapFiled.Type),
		Integer:   zapFiled.Integer,
		String:    zapFiled.String,
		Interface: zapFiled.Interface,
	}
}
