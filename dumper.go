package main

import (
	"crypto/tls"
	"fmt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"net"
	"strings"
)

func color(color string, msg string) string {
	var code string

	switch color {
	case "red":
		code = "\x1b[31m"
	case "green":
		code = "\x1b[32m"
	case "yellow":
		code = "\x1b[33m"
	case "blue":
		code = "\x1b[34m"
	case "magenta":
		code = "\x1b[35m"
	case "cyan":
		code = "\x1b[36m"
	case "gray":
		code = "\x1b[90m"
	}

	return fmt.Sprintf("%s%s\x1b[0m", code, msg)
}

var flagName = map[http2.FrameType]map[http2.Flags]string{
	http2.FrameData: {
		http2.FlagDataEndStream: "END_STREAM",
		http2.FlagDataPadded:    "PADDED",
	},
	http2.FrameHeaders: {
		http2.FlagHeadersEndStream:  "END_STREAM",
		http2.FlagHeadersEndHeaders: "END_HEADERS",
		http2.FlagHeadersPadded:     "PADDED",
		http2.FlagHeadersPriority:   "PRIORITY",
	},
	http2.FrameSettings: {
		http2.FlagSettingsAck: "ACK",
	},
	http2.FramePushPromise: {
		http2.FlagPushPromiseEndHeaders: "END_HEADERS",
		http2.FlagPushPromisePadded:     "PADDED",
	},
	http2.FramePing: {
		http2.FlagPingAck: "ACK",
	},
	http2.FrameContinuation: {
		http2.FlagContinuationEndHeaders: "END_HEADERS",
	},
}

type Frame struct {
	Time         uint64          `json:"time"`
	Remote       bool            `json:"remote"`
	RemoteAddr   string          `json:"remote_addr"`
	RemotePort   uint16          `json:"remote_port"`
	ConnectionID string          `json:"connection_id"`
	StreamID     uint32          `json:"stream_id"`
	Type         http2.FrameType `json:"type"`
	Length       uint32          `json:"length"`
	Flags        []http2.Flags   `json:"flags"`
	Payload      interface{}     `json:"payload"`
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
	Parameters map[http2.SettingID]uint32 `json:"parameters"`
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

type FrameHeaderFields struct {
	HeaderFields map[string]string `json:"header_fields"`
}

type FramePriority struct {
	Priority         bool   `json:"-"`
	StreamDependency uint32 `json:"stream_dependency"`
	Weight           uint8  `json:"weight"`
	Exclusive        bool   `json:"exclusive"`
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

type FrameDumper struct {
	ID         string
	RemoteAddr net.Addr

	remoteFramer *Framer
	originFramer *Framer

	remoteFlowController *FlowController
	originFlowController *FlowController

	indent string
}

func (fd *FrameDumper) Connect() {
	fd.PrintMessage(0, "Connected", nil, true)
}

func (fd *FrameDumper) Close() {
	fd.PrintMessage(0, "Closed", nil, true)
}

func (fd *FrameDumper) DumpConnectionState(state tls.ConnectionState) {
	msg := fmt.Sprintf("Negotiated Protocol: %s", state.NegotiatedProtocol)
	fd.PrintMessage(0, msg, nil, true)
}

func (fd *FrameDumper) DumpFrame(chunk []byte, remote bool) {
	callback := func(frame http2.Frame) error {
		f := fd.DumpFrameHeader(frame)

		switch frame := frame.(type) {
		case *http2.DataFrame:
			f.Payload = fd.DumpDataFrame(frame, remote)
		case *http2.HeadersFrame:
			f.Payload = fd.DumpHeadersFrame(frame, remote)
		case *http2.PriorityFrame:
			f.Payload = fd.DumpPriorityFrame(frame, remote)
		case *http2.RSTStreamFrame:
			f.Payload = fd.DumpRSTStreamFrame(frame, remote)
		case *http2.SettingsFrame:
			f.Payload = fd.DumpSettingsFrame(frame, remote)
		case *http2.PushPromiseFrame:
			f.Payload = fd.DumpPushPromiseFrame(frame, remote)
		case *http2.PingFrame:
			f.Payload = fd.DumpPingFrame(frame, remote)
		case *http2.GoAwayFrame:
			f.Payload = fd.DumpGoAwayFrame(frame, remote)
		case *http2.WindowUpdateFrame:
			f.Payload = fd.DumpWindowUpdateFrame(frame, remote)
		case *http2.ContinuationFrame:
			f.Payload = fd.DumpContinuationFrame(frame, remote)
		}

		fd.PrintFrame(&f, remote)

		return nil
	}

	if remote {
		fd.remoteFramer.ReadFrame(chunk, callback)
	} else {
		fd.originFramer.ReadFrame(chunk, callback)
	}
}

func (fd *FrameDumper) DumpFrameHeader(frame http2.Frame) Frame {
	header := frame.Header()
	frameFlags := header.Flags

	f := Frame{
		StreamID: header.StreamID,
		Type:     header.Type,
		Length:   header.Length,
	}

	if frameFlags > 0 {
		candidateFlags := flagName[header.Type]
		for flag, _ := range candidateFlags {
			if (flag & frameFlags) != 0 {
				f.Flags = append(f.Flags, flag)
			}
		}
	}

	return f
}

func (fd *FrameDumper) DumpDataFrame(frame *http2.DataFrame, remote bool) DataFramePayload {
	p := DataFramePayload{}
	p.WindowSize = FrameWindowSize{}

	size := uint32(len(frame.Data()))
	streamID := frame.Header().StreamID

	var fc *FlowController
	if remote {
		fc = fd.originFlowController
	} else {
		fc = fd.remoteFlowController
	}

	p.WindowSize.Connection = fc.UpdateConnectionWindow(-size)
	p.WindowSize.Stream = fc.UpdateStreamWindow(streamID, -size)

	return p
}

func (fd *FrameDumper) DumpHeadersFrame(frame *http2.HeadersFrame, remote bool) HeadersFramePayload {
	p := HeadersFramePayload{}
	p.Priority = frame.HasPriority()

	if frame.HasPriority() {
		priority := frame.Priority
		p.Exclusive = priority.Exclusive
		p.StreamDependency = priority.StreamDep
		p.Weight = priority.Weight
	}

	var f *Framer
	if remote {
		f = fd.remoteFramer
	} else {
		f = fd.originFramer
	}

	hf := frame.HeaderBlockFragment()
	f.ReadHeader(hf, func(header hpack.HeaderField) {
		if p.HeaderFields == nil {
			p.HeaderFields = map[string]string{}
		}
		p.HeaderFields[header.Name] = header.Value
	})

	return p
}

func (fd *FrameDumper) DumpPriorityFrame(frame *http2.PriorityFrame, remote bool) PriorityFramePayload {
	priority := frame.PriorityParam

	p := PriorityFramePayload{}
	p.Priority = true
	p.Exclusive = priority.Exclusive
	p.StreamDependency = priority.StreamDep
	p.Weight = priority.Weight

	return p
}

func (fd *FrameDumper) DumpRSTStreamFrame(frame *http2.RSTStreamFrame, remote bool) RSTStreamFramePayload {
	p := RSTStreamFramePayload{}
	p.ErrorCode = frame.ErrCode

	return p
}

func (fd *FrameDumper) DumpSettingsFrame(frame *http2.SettingsFrame, remote bool) SettingsFramePayload {
	p := SettingsFramePayload{}

	if frame.IsAck() {
		return p
	}

	windowSize, ok := frame.Value(http2.SettingInitialWindowSize)
	if ok {
		var fc *FlowController
		if remote {
			fc = fd.remoteFlowController
		} else {
			fc = fd.originFlowController
		}
		fc.InitialWindowSize = windowSize
	}

	tableSize, ok := frame.Value(http2.SettingHeaderTableSize)
	if ok {
		var f *Framer
		if remote {
			f = fd.remoteFramer
		} else {
			f = fd.originFramer
		}
		f.SetMaxDynamicTableSize(tableSize)
	}

	frame.ForeachSetting(func(setting http2.Setting) error {
		if p.Parameters == nil {
			p.Parameters = map[http2.SettingID]uint32{}
		}
		p.Parameters[setting.ID] = setting.Val

		return nil
	})

	return p
}

func (fd *FrameDumper) DumpPushPromiseFrame(frame *http2.PushPromiseFrame, remote bool) PushPromiseFramePayload {
	p := PushPromiseFramePayload{}
	p.PromisedStreamID = frame.PromiseID

	var f *Framer
	if remote {
		f = fd.remoteFramer
	} else {
		f = fd.originFramer
	}

	hf := frame.HeaderBlockFragment()
	f.ReadHeader(hf, func(header hpack.HeaderField) {
		if p.HeaderFields == nil {
			p.HeaderFields = map[string]string{}
		}
		p.HeaderFields[header.Name] = header.Value
	})

	return p
}

func (fd *FrameDumper) DumpPingFrame(frame *http2.PingFrame, remote bool) PingFramePayload {
	p := PingFramePayload{}
	if len(frame.Data) > 0 {
		p.OpaqueData = frame.Data
	}

	return p
}

func (fd *FrameDumper) DumpGoAwayFrame(frame *http2.GoAwayFrame, remote bool) GoAwayFramePayload {
	p := GoAwayFramePayload{}
	p.LastStreamID = frame.LastStreamID
	p.ErrorCode = frame.ErrCode

	if len(frame.DebugData()) > 0 {
		p.AdditionalDebugData = frame.DebugData()
	}

	return p
}

func (fd *FrameDumper) DumpWindowUpdateFrame(frame *http2.WindowUpdateFrame, remote bool) WindowUpdateFramePayload {
	p := WindowUpdateFramePayload{}
	p.WindowSize = FrameWindowSize{}
	p.WindowSizeIncrement = frame.Increment

	var fc *FlowController
	if remote {
		fc = fd.remoteFlowController
	} else {
		fc = fd.originFlowController
	}

	size := uint32(frame.Increment)
	streamID := frame.Header().StreamID
	if streamID == 0 {
		p.WindowSize.Connection = fc.UpdateConnectionWindow(size)
	} else {
		p.WindowSize.Stream = fc.UpdateStreamWindow(streamID, size)
	}

	return p
}

func (fd *FrameDumper) DumpContinuationFrame(frame *http2.ContinuationFrame, remote bool) ContinuationFramePayload {
	p := ContinuationFramePayload{}

	var f *Framer
	if remote {
		f = fd.remoteFramer
	} else {
		f = fd.originFramer
	}

	hf := frame.HeaderBlockFragment()
	f.ReadHeader(hf, func(header hpack.HeaderField) {
		if p.HeaderFields == nil {
			p.HeaderFields = map[string]string{}
		}
		p.HeaderFields[header.Name] = header.Value
	})

	return p
}

func (fd *FrameDumper) PrintFrame(frame *Frame, remote bool) {
	var msgColor string

	if remote {
		msgColor = "cyan"
	} else {
		msgColor = "magenta"
	}

	frameType := color(msgColor, frame.Type.String())
	msg := fmt.Sprintf("%s Frame <Length:%d>", frameType, frame.Length)

	data := []string{}

	if len(frame.Flags) > 0 {
		data = append(data, "Flags:")
		for _, f := range frame.Flags {
			data = append(data, fmt.Sprintf("  - %s (0x%x)", flagName[frame.Type][f], f))
		}
	}

	switch payload := frame.Payload.(type) {
	case DataFramePayload:
		var size WindowSize
		data = append(data, "Window Size:")
		size = payload.WindowSize.Connection
		data = append(data, fmt.Sprintf("  Connection: %d (%d)", size.current, size.delta))
		size = payload.WindowSize.Stream
		data = append(data, fmt.Sprintf("  Stream: %d (%d)", size.current, size.delta))

	case HeadersFramePayload:
		if payload.Priority {
			var exclusive string

			if payload.Exclusive {
				exclusive = "Yes"
			} else {
				exclusive = "No"
			}

			data = append(data, fmt.Sprintf("Stream Dependency: %d", payload.StreamDependency))
			data = append(data, fmt.Sprintf("Weight: %d", payload.Weight))
			data = append(data, fmt.Sprintf("Exclusive: %s", exclusive))
		}

		if len(payload.HeaderFields) > 0 {
			data = append(data, "Header Fields:")
			for k, v := range payload.HeaderFields {
				data = append(data, fmt.Sprintf("  %s: %s", k, v))
			}
		}

	case PriorityFramePayload:
		var exclusive string

		if payload.Exclusive {
			exclusive = "Yes"
		} else {
			exclusive = "No"
		}

		data = append(data, fmt.Sprintf("Stream Dependency: %d", payload.StreamDependency))
		data = append(data, fmt.Sprintf("Weight: %d", payload.Weight))
		data = append(data, fmt.Sprintf("Exclusive: %s", exclusive))

	case RSTStreamFramePayload:
		data = append(data, fmt.Sprintf("Error Code: %s (0x%d)", payload.ErrorCode.String(), payload.ErrorCode))

	case SettingsFramePayload:
		if len(payload.Parameters) > 0 {
			data = append(data, "Parameters:")
			for s, v := range payload.Parameters {
				data = append(data, fmt.Sprintf("  %s (0x%d): %d", s.String(), s, v))
			}
		}

	case PushPromiseFramePayload:
		data = append(data, fmt.Sprintf("Promised Stream ID: %d", payload.PromisedStreamID))
		if len(payload.HeaderFields) > 0 {
			data = append(data, "Header Fields:")
			for k, v := range payload.HeaderFields {
				data = append(data, fmt.Sprintf("  %s: %s", k, v))
			}
		}

	case PingFramePayload:
		if len(payload.OpaqueData) > 0 {
			data = append(data, fmt.Sprintf("Opaque Data: 0x%x", payload.OpaqueData))
		}

	case GoAwayFramePayload:
		data = append(data, fmt.Sprintf("Last Stream ID: %d", payload.LastStreamID))
		data = append(data, fmt.Sprintf("Error Code: %d", payload.ErrorCode))
		if len(payload.AdditionalDebugData) > 0 {
			data = append(data, fmt.Sprintf("Additional Debug Data: 0x%x", payload.AdditionalDebugData))
		}

	case WindowUpdateFramePayload:
		var size WindowSize
		data = append(data, "Window Size:")
		size = payload.WindowSize.Connection
		data = append(data, fmt.Sprintf("  Connection: %d (%d)", size.current, size.delta))
		size = payload.WindowSize.Stream
		data = append(data, fmt.Sprintf("  Stream: %d (%d)", size.current, size.delta))

	case ContinuationFramePayload:
		if len(payload.HeaderFields) > 0 {
			data = append(data, "Header Fields:")
			for k, v := range payload.HeaderFields {
				data = append(data, fmt.Sprintf("  %s: %s", k, v))
			}
		}
	}

	fd.PrintMessage(frame.StreamID, msg, data, remote)
}

func (fd *FrameDumper) PrintMessage(streamID uint32, msg string, data []string, remote bool) {
	var flowStr string

	if remote {
		flowStr = color("cyan", "==>")
	} else {
		flowStr = color("magenta", "<==")
	}
	delimiter := color("gray", "|")

	log := []string{}
	log = append(log, fmt.Sprintf("%s [%s] [%3d] %s", flowStr, fd.ID, streamID, msg))
	for _, d := range data {
		log = append(log, fmt.Sprintf("%s%s %s", fd.indent, delimiter, d))
	}

	fmt.Println(strings.Join(log, "\n"))
}

func NewFrameDumper(addr net.Addr) *FrameDumper {
	dumper := &FrameDumper{
		ID:         addr.String(),
		RemoteAddr: addr,

		remoteFramer: NewFramer(true),
		originFramer: NewFramer(false),

		remoteFlowController: NewFlowController(),
		originFlowController: NewFlowController(),

		indent: strings.Repeat(" ", 28),
	}

	dumper.Connect()

	return dumper
}
