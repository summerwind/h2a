package main

import (
	"fmt"
	"golang.org/x/net/http2"
	"net"
	"time"
)

type Frame struct {
	Time         int64         `json:"time"`
	Remote       bool          `json:"remote"`
	RemoteAddr   net.IP        `json:"remote_addr"`
	RemotePort   int           `json:"remote_port"`
	ConnectionID string        `json:"connection_id"`
	StreamID     uint32        `json:"stream_id"`
	Length       uint32        `json:"length"`
	Type         FrameNameID   `json:"type"`
	Flags        []FrameNameID `json:"flags"`
	Payload      FramePayload  `json:"payload"`
}

type FramePayload interface{}

type DataFramePayload struct {
	FrameWindowSizeGroup
}

type HeadersFramePayload struct {
	FramePriority
	FrameHeaderFields
}

type PriorityFramePayload struct {
	FramePriority
}

type RSTStreamFramePayload struct {
	FrameErrorCode
}

type SettingsFramePayload struct {
	Parameters map[string]FrameSetting `json:"parameters"`
}

type PushPromiseFramePayload struct {
	PromisedStreamID uint32 `json:"promised_stream_id"`
	FrameHeaderFields
}

type PingFramePayload struct {
	OpaqueData [8]byte `json:"opaque_data"`
}

type GoAwayFramePayload struct {
	LastStreamID        uint32 `json:"last_stream_id"`
	AdditionalDebugData []byte `json:"additional_debug_data"`
	FrameErrorCode
}

type WindowUpdateFramePayload struct {
	WindowSizeIncrement uint32 `json:"window_size_increment"`
	FrameWindowSizeGroup
}

type ContinuationFramePayload struct {
	FrameHeaderFields
}

type FrameNameID struct {
	Name string
	ID   uint8
}

func (fni FrameNameID) String() string {
	return fmt.Sprintf("%s (0x%x)", fni.Name, fni.ID)
}

func (fni FrameNameID) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", fni.Name)), nil
}

type FrameHeaderFields struct {
	HeaderFields map[string]string `json:"header_fields"`
}

type FramePriority struct {
	Priority         bool   `json:"-"`
	StreamDependency uint32 `json:"stream_dependency"`
	Weight           uint8  `json:"weight"`
	Exclusive        bool   `json:"exclusive"`
}

type FrameSetting struct {
	Name  string
	Value uint32
	ID    uint16
}

func (fs FrameSetting) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%d", fs.Value)), nil
}

type FrameErrorCode struct {
	ErrorCode http2.ErrCode `json:"error_code"`
}

type FrameWindowSizeGroup struct {
	WindowSize FrameWindowSize `json:"window_size"`
}

type FrameWindowSize struct {
	Connection WindowSize `json:"connection"`
	Stream     WindowSize `json:"stream"`
}

func NewFrame() *Frame {
	return &Frame{
		Time:  time.Now().UnixNano(),
		Flags: []FrameNameID{},
	}
}
