package main

import (
	"crypto/tls"
	"encoding/json"
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

type Formatter int

const (
	DefaultFormatter Formatter = iota
	JSONFormatter
)

type FrameDumper struct {
	ID         string
	RemoteAddr net.Addr
	Formatter  Formatter

	remoteFramer *Framer
	originFramer *Framer

	remoteFlowController *FlowController
	originFlowController *FlowController

	indent string
}

func (fd *FrameDumper) Connect() {
	e := NewEvent(EventConnect, true, fd.RemoteAddr, 0)
	e.Message = "Connected"
	fd.PrintEvent(e)
}

func (fd *FrameDumper) Close() {
	e := NewEvent(EventClose, true, fd.RemoteAddr, 0)
	e.Message = "Closed"
	fd.PrintEvent(e)
}

func (fd *FrameDumper) DumpConnectionState(state tls.ConnectionState) {
	e := NewEvent(EventConnectionState, true, fd.RemoteAddr, 0)
	e.State = NewState(state.NegotiatedProtocol)
	fd.PrintEvent(e)
}

func (fd *FrameDumper) DumpFrame(chunk []byte, remote bool) {
	callback := func(frame http2.Frame) error {
		e := NewEvent(EventFrame, remote, fd.RemoteAddr, frame.Header().StreamID)
		e.Frame = fd.DumpFrameHeader(frame, remote)

		switch frame := frame.(type) {
		case *http2.DataFrame:
			e.Frame.Payload = fd.DumpDataFrame(frame, remote)
		case *http2.HeadersFrame:
			e.Frame.Payload = fd.DumpHeadersFrame(frame, remote)
		case *http2.PriorityFrame:
			e.Frame.Payload = fd.DumpPriorityFrame(frame, remote)
		case *http2.RSTStreamFrame:
			e.Frame.Payload = fd.DumpRSTStreamFrame(frame, remote)
		case *http2.SettingsFrame:
			e.Frame.Payload = fd.DumpSettingsFrame(frame, remote)
		case *http2.PushPromiseFrame:
			e.Frame.Payload = fd.DumpPushPromiseFrame(frame, remote)
		case *http2.PingFrame:
			e.Frame.Payload = fd.DumpPingFrame(frame, remote)
		case *http2.GoAwayFrame:
			e.Frame.Payload = fd.DumpGoAwayFrame(frame, remote)
		case *http2.WindowUpdateFrame:
			e.Frame.Payload = fd.DumpWindowUpdateFrame(frame, remote)
		case *http2.ContinuationFrame:
			e.Frame.Payload = fd.DumpContinuationFrame(frame, remote)
		}

		fd.PrintEvent(e)

		return nil
	}

	if remote {
		fd.remoteFramer.ReadFrame(chunk, callback)
	} else {
		fd.originFramer.ReadFrame(chunk, callback)
	}
}

func (fd *FrameDumper) DumpFrameHeader(frame http2.Frame, remote bool) *Frame {
	header := frame.Header()

	f := NewFrame()
	f.Length = header.Length
	f.Type = FrameNameID{
		ID:   uint8(header.Type),
		Name: header.Type.String(),
	}

	frameFlags := header.Flags
	if frameFlags > 0 {
		candidateFlags := flagName[header.Type]
		for flag, _ := range candidateFlags {
			if (flag & frameFlags) != 0 {
				fni := FrameNameID{
					ID:   uint8(flag),
					Name: flagName[header.Type][flag],
				}
				f.Flags = append(f.Flags, fni)
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
			p.Parameters = map[string]FrameSetting{}
		}

		fs := FrameSetting{
			Name:  setting.ID.String(),
			Value: setting.Val,
			ID:    uint16(setting.ID),
		}
		p.Parameters[fs.Name] = fs

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

func (fd *FrameDumper) PrintEvent(e *Event) {
	if fd.Formatter == JSONFormatter {
		j, err := json.Marshal(e)
		if err != nil {
			logger.Printf("JSON Error: %s\n", err)
		} else {
			fmt.Println(string(j))
		}
		return
	}

	switch e.Type {
	case EventFrame:
		fd.PrintFrame(e)
	case EventConnectionState:
		fd.PrintConnectionState(e)
	default:
		fd.PrintMessage(e.StreamID, e.Message, nil, e.Remote)
	}
}

func (fd *FrameDumper) PrintFrame(e *Event) {
	frame := e.Frame

	var msgColor string
	if e.Remote {
		msgColor = "cyan"
	} else {
		msgColor = "magenta"
	}

	frameType := color(msgColor, frame.Type.Name)
	msg := fmt.Sprintf("%s Frame <Length:%d>", frameType, frame.Length)

	data := []string{}

	if len(frame.Flags) > 0 {
		data = append(data, "Flags:")
		for _, f := range frame.Flags {
			data = append(data, fmt.Sprintf("  - %s (0x%x)", f.Name, f.ID))
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
			for _, s := range payload.Parameters {
				data = append(data, fmt.Sprintf("  %s (0x%d): %d", s.Name, s.ID, s.Value))
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

	fd.PrintMessage(e.StreamID, msg, data, e.Remote)
}

func (fd *FrameDumper) PrintConnectionState(e *Event) {
	msg := fmt.Sprintf("Negotiated Protocol: %s", e.State.NegotiatedProtocol)
	fd.PrintMessage(e.StreamID, msg, nil, e.Remote)
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

func NewFrameDumper(addr net.Addr, formatter Formatter) *FrameDumper {
	dumper := &FrameDumper{
		ID:         addr.String(),
		RemoteAddr: addr,
		Formatter:  formatter,

		remoteFramer: NewFramer(true),
		originFramer: NewFramer(false),

		remoteFlowController: NewFlowController(),
		originFlowController: NewFlowController(),

		indent: strings.Repeat(" ", 28),
	}

	dumper.Connect()

	return dumper
}
