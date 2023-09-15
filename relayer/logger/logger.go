package logger

import "go.uber.org/zap"

// A Field is a marshaling operation used to add a key-value pair to a logger's context. Most fields are lazily
// marshaled, so it's inexpensive to add fields to disabled debug-level log statements.
type Field struct {
	Key       string
	Type      FieldType
	Integer   int64
	String    string
	Interface interface{}
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

// NewFiled takes a key and an arbitrary value and chooses the best way to represent them as a field, falling back to a
// reflection-based approach only if necessary.
func NewFiled(key string, value interface{}) Field {
	zapFiled := zap.Any(key, value)
	return Field{
		Key:       zapFiled.Key,
		Type:      FieldType(zapFiled.Type),
		Integer:   zapFiled.Integer,
		String:    zapFiled.String,
		Interface: zapFiled.Interface,
	}
}
