package json

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"sync"
	"unsafe"
)

// A Token holds a value of one of these types:
//
//	Delim, for the four JSON delimiters [ ] { }
//	bool, for JSON booleans
//	float64, for JSON numbers
//	Number, for JSON numbers
//	string, for JSON string literals
//	nil, for JSON null
//
type Token interface{}

type Delim rune

type decoder interface {
	decode(*context, uintptr) error
}

type Decoder struct {
	r        io.Reader
	buffered func() io.Reader
}

var (
	ctxPool       sync.Pool
	cachedDecoder map[string]decoder
)

func init() {
	cachedDecoder = map[string]decoder{}
	ctxPool = sync.Pool{
		New: func() interface{} {
			return newContext()
		},
	}
}

// NewDecoder returns a new decoder that reads from r.
//
// The decoder introduces its own buffering and may
// read data from r beyond the JSON values requested.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Buffered returns a reader of the data remaining in the Decoder's
// buffer. The reader is valid until the next call to Decode.
func (d *Decoder) Buffered() io.Reader {
	return d.buffered()
}

func (d *Decoder) decode(src []byte, header *interfaceHeader) error {
	typ := header.typ
	if typ.Kind() != reflect.Ptr {
		return ErrDecodePointer
	}
	name := typ.String()
	dec, exists := cachedDecoder[name]
	if !exists {
		compiledDec, err := d.compile(typ.Elem())
		if err != nil {
			return err
		}
		if name != "" {
			cachedDecoder[name] = compiledDec
		}
		dec = compiledDec
	}
	ptr := uintptr(header.ptr)
	ctx := ctxPool.Get().(*context)
	ctx.setBuf(src)
	if err := dec.decode(ctx, ptr); err != nil {
		ctxPool.Put(ctx)
		return err
	}
	ctxPool.Put(ctx)
	return nil
}

func (d *Decoder) decodeForUnmarshal(src []byte, v interface{}) error {
	header := (*interfaceHeader)(unsafe.Pointer(&v))
	header.typ.escape()
	return d.decode(src, header)
}

func (d *Decoder) decodeForUnmarshalNoEscape(src []byte, v interface{}) error {
	header := (*interfaceHeader)(unsafe.Pointer(&v))
	return d.decode(src, header)
}

// Decode reads the next JSON-encoded value from its
// input and stores it in the value pointed to by v.
//
// See the documentation for Unmarshal for details about
// the conversion of JSON into a Go value.
func (d *Decoder) Decode(v interface{}) error {
	header := (*interfaceHeader)(unsafe.Pointer(&v))
	typ := header.typ
	if typ.Kind() != reflect.Ptr {
		return ErrDecodePointer
	}
	name := typ.String()
	dec, exists := cachedDecoder[name]
	if !exists {
		compiledDec, err := d.compile(typ.Elem())
		if err != nil {
			return err
		}
		if name != "" {
			cachedDecoder[name] = compiledDec
		}
		dec = compiledDec
	}
	ptr := uintptr(header.ptr)
	ctx := ctxPool.Get().(*context)
	defer ctxPool.Put(ctx)
	d.buffered = func() io.Reader {
		return bytes.NewReader(ctx.buf[ctx.cursor:])
	}
	for {
		buf := make([]byte, 1024)
		n, err := d.r.Read(buf)
		if n == 0 || err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		ctx.setBuf(buf[:n])
		if err := dec.decode(ctx, ptr); err != nil {
			return err
		}
	}
	return nil
}

func (d *Decoder) compile(typ *rtype) (decoder, error) {
	switch typ.Kind() {
	case reflect.Ptr:
		return d.compilePtr(typ)
	case reflect.Struct:
		return d.compileStruct(typ)
	case reflect.Slice:
		return d.compileSlice(typ)
	case reflect.Array:
		return d.compileArray(typ)
	case reflect.Int:
		return d.compileInt()
	case reflect.Int8:
		return d.compileInt8()
	case reflect.Int16:
		return d.compileInt16()
	case reflect.Int32:
		return d.compileInt32()
	case reflect.Int64:
		return d.compileInt64()
	case reflect.Uint:
		return d.compileUint()
	case reflect.Uint8:
		return d.compileUint8()
	case reflect.Uint16:
		return d.compileUint16()
	case reflect.Uint32:
		return d.compileUint32()
	case reflect.Uint64:
		return d.compileUint64()
	case reflect.String:
		return d.compileString()
	case reflect.Bool:
		return d.compileBool()
	case reflect.Float32:
		return d.compileFloat32()
	case reflect.Float64:
		return d.compileFloat64()
	}
	return nil, nil
}

func (d *Decoder) compilePtr(typ *rtype) (decoder, error) {
	dec, err := d.compile(typ.Elem())
	if err != nil {
		return nil, err
	}
	return newPtrDecoder(dec, typ.Elem()), nil
}

func (d *Decoder) compileInt() (decoder, error) {
	return newIntDecoder(func(p uintptr, v int64) {
		*(*int)(unsafe.Pointer(p)) = int(v)
	}), nil
}

func (d *Decoder) compileInt8() (decoder, error) {
	return newIntDecoder(func(p uintptr, v int64) {
		*(*int8)(unsafe.Pointer(p)) = int8(v)
	}), nil
}

func (d *Decoder) compileInt16() (decoder, error) {
	return newIntDecoder(func(p uintptr, v int64) {
		*(*int16)(unsafe.Pointer(p)) = int16(v)
	}), nil
}

func (d *Decoder) compileInt32() (decoder, error) {
	return newIntDecoder(func(p uintptr, v int64) {
		*(*int32)(unsafe.Pointer(p)) = int32(v)
	}), nil
}

func (d *Decoder) compileInt64() (decoder, error) {
	return newIntDecoder(func(p uintptr, v int64) {
		*(*int64)(unsafe.Pointer(p)) = v
	}), nil
}

func (d *Decoder) compileUint() (decoder, error) {
	return newUintDecoder(func(p uintptr, v uint64) {
		*(*uint)(unsafe.Pointer(p)) = uint(v)
	}), nil
}

func (d *Decoder) compileUint8() (decoder, error) {
	return newUintDecoder(func(p uintptr, v uint64) {
		*(*uint8)(unsafe.Pointer(p)) = uint8(v)
	}), nil
}

func (d *Decoder) compileUint16() (decoder, error) {
	return newUintDecoder(func(p uintptr, v uint64) {
		*(*uint16)(unsafe.Pointer(p)) = uint16(v)
	}), nil
}

func (d *Decoder) compileUint32() (decoder, error) {
	return newUintDecoder(func(p uintptr, v uint64) {
		*(*uint32)(unsafe.Pointer(p)) = uint32(v)
	}), nil
}

func (d *Decoder) compileUint64() (decoder, error) {
	return newUintDecoder(func(p uintptr, v uint64) {
		*(*uint64)(unsafe.Pointer(p)) = v
	}), nil
}

func (d *Decoder) compileFloat32() (decoder, error) {
	return newFloatDecoder(func(p uintptr, v float64) {
		*(*float32)(unsafe.Pointer(p)) = float32(v)
	}), nil
}

func (d *Decoder) compileFloat64() (decoder, error) {
	return newFloatDecoder(func(p uintptr, v float64) {
		*(*float64)(unsafe.Pointer(p)) = v
	}), nil
}

func (d *Decoder) compileString() (decoder, error) {
	return newStringDecoder(), nil
}

func (d *Decoder) compileBool() (decoder, error) {
	return newBoolDecoder(), nil
}

func (d *Decoder) compileSlice(typ *rtype) (decoder, error) {
	elem := typ.Elem()
	decoder, err := d.compile(elem)
	if err != nil {
		return nil, err
	}
	return newSliceDecoder(decoder, elem, elem.Size()), nil
}

func (d *Decoder) compileArray(typ *rtype) (decoder, error) {
	elem := typ.Elem()
	decoder, err := d.compile(elem)
	if err != nil {
		return nil, err
	}
	return newArrayDecoder(decoder, elem, typ.Len()), nil
}

func (d *Decoder) getTag(field reflect.StructField) string {
	return field.Tag.Get("json")
}

func (d *Decoder) isIgnoredStructField(field reflect.StructField) bool {
	if field.PkgPath != "" && !field.Anonymous {
		// private field
		return true
	}
	tag := d.getTag(field)
	if tag == "-" {
		return true
	}
	return false
}

func (d *Decoder) compileStruct(typ *rtype) (decoder, error) {
	fieldNum := typ.NumField()
	fieldMap := map[string]*structFieldSet{}
	for i := 0; i < fieldNum; i++ {
		field := typ.Field(i)
		if d.isIgnoredStructField(field) {
			continue
		}
		keyName := field.Name
		tag := d.getTag(field)
		opts := strings.Split(tag, ",")
		if len(opts) > 0 {
			if opts[0] != "" {
				keyName = opts[0]
			}
		}
		dec, err := d.compile(type2rtype(field.Type))
		if err != nil {
			return nil, err
		}
		fieldSet := &structFieldSet{dec: dec, offset: field.Offset}
		fieldMap[field.Name] = fieldSet
		fieldMap[keyName] = fieldSet
		fieldMap[strings.ToLower(keyName)] = fieldSet
	}
	return newStructDecoder(fieldMap), nil
}

// DisallowUnknownFields causes the Decoder to return an error when the destination
// is a struct and the input contains object keys which do not match any
// non-ignored, exported fields in the destination.
func (d *Decoder) DisallowUnknownFields() {

}

func (d *Decoder) InputOffset() int64 {
	return 0
}

func (d *Decoder) More() bool {
	return false
}

func (d *Decoder) Token() (Token, error) {
	return nil, nil
}

// UseNumber causes the Decoder to unmarshal a number into an interface{} as a
// Number instead of as a float64.
func (d *Decoder) UseNumber() {

}