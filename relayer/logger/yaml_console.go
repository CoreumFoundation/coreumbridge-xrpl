package logger

import (
	"encoding/hex"
	"reflect"
	"strconv"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

// YamlConsoleLoggerFormat is yaml console log formatter types, used for the CLI logging.
const YamlConsoleLoggerFormat = "yaml-console"

var bufPool = buffer.NewPool()

func init() {
	if err := zap.RegisterEncoder(YamlConsoleLoggerFormat, func(config zapcore.EncoderConfig) (zapcore.Encoder, error) {
		return newYamlConsoleEncoder(0), nil
	}); err != nil {
		panic(errors.Wrapf(err, "failed to set %s logger encoder", YamlConsoleLoggerFormat))
	}
}

func newYamlConsoleEncoder(nested int) *yamlConsoleEncoder {
	return &yamlConsoleEncoder{
		nested: nested,
		buffer: bufPool.Get(),
	}
}

type yamlConsoleEncoder struct {
	nested                 int
	element                int
	ignoreFirstIndentation bool
	array                  bool
	skipErrorStackTrace    bool
	containsStackTrace     bool
	buffer                 *buffer.Buffer
}

func (c *yamlConsoleEncoder) AddArray(key string, marshaller zapcore.ArrayMarshaler) error {
	c.addKey(key)
	c.buffer.AppendByte('\n')
	return c.AppendArray(marshaller)
}

func (c *yamlConsoleEncoder) AddObject(key string, marshaller zapcore.ObjectMarshaler) error {
	c.addKey(key)
	c.buffer.AppendByte('\n')
	return c.AppendObject(marshaller)
}

func (c *yamlConsoleEncoder) AddBinary(key string, value []byte) {
	c.addKey(key)
	c.buffer.AppendByte('"')
	c.buffer.AppendString(hex.EncodeToString(value))
	c.buffer.AppendByte('"')
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddByteString(key string, value []byte) {
	c.addKey(key)
	c.buffer.AppendString(string(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddBool(key string, value bool) {
	c.addKey(key)
	c.buffer.AppendBool(value)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddComplex128(key string, value complex128) {
	c.addKey(key)
	c.appendComplex128(value)
}

func (c *yamlConsoleEncoder) AddComplex64(key string, value complex64) {
	c.addKey(key)
	c.appendComplex128(complex128(value))
}

func (c *yamlConsoleEncoder) AddDuration(key string, value time.Duration) {
	c.addKey(key)
	c.buffer.AppendString(value.String())
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddFloat64(key string, value float64) {
	c.addKey(key)
	c.buffer.AppendFloat(value, 64)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddFloat32(key string, value float32) {
	c.addKey(key)
	c.buffer.AppendFloat(float64(value), 32)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddInt(key string, value int) {
	c.addKey(key)
	c.buffer.AppendInt(int64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddInt64(key string, value int64) {
	c.addKey(key)
	c.buffer.AppendInt(value)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddInt32(key string, value int32) {
	c.addKey(key)
	c.buffer.AppendInt(int64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddInt16(key string, value int16) {
	c.addKey(key)
	c.buffer.AppendInt(int64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddInt8(key string, value int8) {
	c.addKey(key)
	c.buffer.AppendInt(int64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddString(key, value string) {
	c.addKey(key)
	appendString(c.buffer, value, c.indentation())
}

func (c *yamlConsoleEncoder) AddTime(key string, value time.Time) {
	c.addKey(key)
	c.buffer.AppendTime(value.UTC(), "2006-01-02 15:04:05.000")
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddUint(key string, value uint) {
	c.addKey(key)
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddUint64(key string, value uint64) {
	c.addKey(key)
	c.buffer.AppendUint(value)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddUint32(key string, value uint32) {
	c.addKey(key)
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddUint16(key string, value uint16) {
	c.addKey(key)
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddUint8(key string, value uint8) {
	c.addKey(key)
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddUintptr(key string, value uintptr) {
	c.addKey(key)
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AddReflected(key string, value any) error {
	c.addKey(key)
	return c.AppendReflected(value)
}

func (c *yamlConsoleEncoder) OpenNamespace(key string) {
	c.nested = 0
	c.addKey(key)
	c.buffer.AppendByte('\n')
	c.nested = 1
}

func (c *yamlConsoleEncoder) Clone() zapcore.Encoder {
	buf := bufPool.Get()
	if _, err := buf.Write(c.buffer.Bytes()); err != nil {
		panic(errors.Wrapf(err, "failed to write buffer"))
	}

	return &yamlConsoleEncoder{
		nested:              c.nested,
		array:               c.array,
		skipErrorStackTrace: c.skipErrorStackTrace,
		containsStackTrace:  c.containsStackTrace,
		buffer:              buf,
	}
}

func (c *yamlConsoleEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	buf := bufPool.Get()
	if entry.Message != "" {
		buf.AppendString(entry.Message)
	}
	if c.buffer.Len() > 0 {
		if _, err := buf.Write(c.buffer.Bytes()); err != nil {
			panic(errors.Wrapf(err, "failed to write buffer"))
		}
	}

	subEncoder := newYamlConsoleEncoder(0)
	if entry.Level == zap.InfoLevel {
		subEncoder.skipErrorStackTrace = true
	}
	defer subEncoder.buffer.Free()

	buf.AppendString("\n")
	for _, field := range fields {
		if !subEncoder.appendError(field) {
			field.AddTo(subEncoder)
		}
	}
	if _, err := buf.Write(subEncoder.buffer.Bytes()); err != nil {
		return nil, errors.Wrapf(err, "failed to write buffer")
	}
	if !c.containsStackTrace && !subEncoder.containsStackTrace && entry.Stack != "" {
		buf.AppendString(`      stack: `)
		appendString(buf, entry.Stack, "  ")
	}
	return buf, nil
}

func (c *yamlConsoleEncoder) AppendBool(value bool) {
	c.addComma()
	c.buffer.AppendBool(value)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendByteString(value []byte) {
	c.addComma()
	appendString(c.buffer, string(value), c.indentation())
}

func (c *yamlConsoleEncoder) AppendComplex128(value complex128) {
	c.addComma()
	c.appendComplex128(value)
}

func (c *yamlConsoleEncoder) AppendComplex64(value complex64) {
	c.addComma()
	c.appendComplex128(complex128(value))
}

func (c *yamlConsoleEncoder) AppendFloat64(value float64) {
	c.addComma()
	c.buffer.AppendFloat(value, 64)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendFloat32(value float32) {
	c.addComma()
	c.buffer.AppendFloat(float64(value), 32)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendInt(value int) {
	c.addComma()
	c.buffer.AppendInt(int64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendInt64(value int64) {
	c.addComma()
	c.buffer.AppendInt(value)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendInt32(value int32) {
	c.addComma()
	c.buffer.AppendInt(int64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendInt16(value int16) {
	c.addComma()
	c.buffer.AppendInt(int64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendInt8(value int8) {
	c.addComma()
	c.buffer.AppendInt(int64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendString(value string) {
	c.addComma()
	appendString(c.buffer, value, c.indentation())
}

func (c *yamlConsoleEncoder) AppendUint(value uint) {
	c.addComma()
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendUint64(value uint64) {
	c.addComma()
	c.buffer.AppendUint(value)
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendUint32(value uint32) {
	c.addComma()
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendUint16(value uint16) {
	c.addComma()
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendUint8(value uint8) {
	c.addComma()
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendUintptr(value uintptr) {
	c.addComma()
	c.buffer.AppendUint(uint64(value))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendDuration(value time.Duration) {
	c.addComma()
	c.buffer.AppendString(value.String())
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendTime(value time.Time) {
	c.addComma()
	c.buffer.AppendTime(value.UTC(), "2006-01-02 15:04:05.000")
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) AppendArray(marshaller zapcore.ArrayMarshaler) error {
	subEncoder := newYamlConsoleEncoder(c.nested + 1)
	subEncoder.array = true
	defer subEncoder.buffer.Free()

	if err := marshaller.MarshalLogArray(subEncoder); err != nil {
		return errors.WithStack(err)
	}

	c.addComma()
	if _, err := c.buffer.Write(subEncoder.buffer.Bytes()); err != nil {
		return errors.Wrapf(err, "failed to write buffer")
	}
	return nil
}

func (c *yamlConsoleEncoder) AppendObject(marshaller zapcore.ObjectMarshaler) error {
	subEncoder := newYamlConsoleEncoder(c.nested + 1)
	if c.array {
		subEncoder.nested--
		subEncoder.ignoreFirstIndentation = true
	}
	defer subEncoder.buffer.Free()

	if err := marshaller.MarshalLogObject(subEncoder); err != nil {
		return errors.WithStack(err)
	}

	c.addComma()
	if _, err := c.buffer.Write(subEncoder.buffer.Bytes()); err != nil {
		return errors.Wrapf(err, "failed to write buffer")
	}
	return nil
}

func (c *yamlConsoleEncoder) AppendReflected(value any) error {
	if appended := c.appendCustomTypes(value); appended {
		return nil
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Invalid:
		c.appendNil()
	case reflect.Bool:
		c.AppendBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		c.AppendInt64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		c.AppendUint64(v.Uint())
	case reflect.Float32, reflect.Float64:
		c.AppendFloat64(v.Float())
	case reflect.Complex64, reflect.Complex128:
		c.AppendComplex128(v.Complex())
	case reflect.Array:
		c.buffer.AppendByte('\n')
		return c.appendReflectedSequence(v)
	case reflect.Slice:
		if v.IsNil() {
			c.appendNil()
		} else {
			c.buffer.AppendByte('\n')
			return c.appendReflectedSequence(v)
		}
	case reflect.Map:
		if v.IsNil() {
			c.appendNil()
		} else {
			c.buffer.AppendByte('\n')
			return c.appendReflectedMapping(v)
		}
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			c.appendNil()
		} else {
			return c.AppendReflected(v.Elem().Interface())
		}
	case reflect.Struct:
		c.buffer.AppendByte('\n')
		return c.appendReflectedStruct(v)
	case reflect.String:
		c.AppendString(v.String())
	default:
		return errors.Errorf("unable to serialize %s", v.Kind())
	}
	return nil
}

func (c *yamlConsoleEncoder) appendCustomTypes(value any) bool {
	switch v := value.(type) {
	case sdk.AccAddress:
		c.AppendString(v.String())
	case sdkmath.Int:
		c.AppendString(v.String())
	default:
		return false
	}

	return true
}

func (c *yamlConsoleEncoder) indentation() string {
	var res string
	for i := 0; i < c.nested; i++ {
		res += "  "
	}
	return res
}

func (c *yamlConsoleEncoder) addComma() {
	if c.array {
		c.buffer.AppendString(c.indentation())
		c.buffer.AppendString("  - ")
	}
}

func (c *yamlConsoleEncoder) addKey(key string) {
	if !c.ignoreFirstIndentation || c.element > 0 {
		c.buffer.AppendString(c.indentation())
		c.buffer.AppendString("    ")
	}
	c.buffer.AppendString(key)
	c.buffer.AppendString(": ")
	c.element++
}

func (c *yamlConsoleEncoder) appendNil() {
	c.buffer.AppendString("null\n")
}

func (c *yamlConsoleEncoder) appendError(field zapcore.Field) bool {
	if field.Type != zapcore.ErrorType {
		return false
	}
	c.addKey(field.Key)

	ind := "\n" + c.indentation()
	err := field.Interface.(error)
	c.buffer.AppendString(ind)
	c.buffer.AppendString("      msg: ")
	c.buffer.AppendByte('"')
	c.buffer.AppendString(err.Error())
	c.buffer.AppendString("\"\n")

	if c.skipErrorStackTrace {
		return false
	}

	errStack, ok := err.(stackTracer) //nolint:errorlint // we check interface, not error here
	if !ok {
		return false
	}
	stack := errStack.StackTrace()

	if len(stack) == 0 {
		return false
	}

	c.buffer.AppendString("      stack:")
	for _, frame := range stack {
		c.buffer.AppendString(ind)
		c.buffer.AppendString("      - \"")
		text, err := frame.MarshalText()
		if err != nil {
			panic(errors.Wrapf(err, "failed to marshal frame to text"))
		}
		c.buffer.AppendString(string(text))
		c.buffer.AppendByte('"')
	}
	c.buffer.AppendByte('\n')
	c.containsStackTrace = true
	return true
}

func (c *yamlConsoleEncoder) appendComplex128(value complex128) {
	re, im := real(value), imag(value)
	c.buffer.AppendString(strconv.FormatFloat(re, 'g', -1, 64))
	if im >= 0 {
		c.buffer.AppendString("+")
	}
	c.buffer.AppendString(strconv.FormatFloat(im, 'g', -1, 64))
	c.buffer.AppendByte('\n')
}

func (c *yamlConsoleEncoder) appendReflectedSequence(v reflect.Value) error {
	return c.AppendArray(zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
		n := v.Len()
		for i := 0; i < n; i++ {
			if err := enc.AppendReflected(v.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	}))
}

func (c *yamlConsoleEncoder) appendReflectedMapping(v reflect.Value) error {
	return c.AppendObject(zapcore.ObjectMarshalerFunc(func(enc zapcore.ObjectEncoder) error {
		iter := v.MapRange()
		for iter.Next() {
			if err := enc.AddReflected(iter.Key().String(), iter.Value().Interface()); err != nil {
				return err
			}
		}
		return nil
	}))
}

func (c *yamlConsoleEncoder) appendReflectedStruct(v reflect.Value) error {
	return c.AppendObject(zapcore.ObjectMarshalerFunc(func(enc zapcore.ObjectEncoder) error {
		t := v.Type()
		n := t.NumField()
		for i := 0; i < n; i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			if err := enc.AddReflected(f.Name, v.FieldByIndex(f.Index).Interface()); err != nil {
				return err
			}
		}
		return nil
	}))
}

func appendString(buffer *buffer.Buffer, value, indentation string) {
	if strings.Contains(value, "\n") {
		buffer.AppendString("\n")
		buffer.AppendString(indentation)
		buffer.AppendString("      \"")
		buffer.AppendString(strings.ReplaceAll(value, "\n", "\n       "+indentation))
	} else {
		buffer.AppendByte('"')
		buffer.AppendString(value)
	}
	buffer.AppendByte('"')
	buffer.AppendByte('\n')
}

type stackTracer interface {
	StackTrace() errors.StackTrace
}
