package codec

import (
	"io"
)

const (
	GodType  Type = "application/gob"
	JsonType Type = "application/json"
)

var NewCodecFuncMap map[Type]NewCodecFunc

type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type Header struct {
	ServiceMethod string
	Seq           uint64
	Error         string
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GodType] = NewGobCodec
}
