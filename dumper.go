package main

import (
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

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

type FrameDumper struct {
	ConnectionID string

	remoteFramer *Framer
	originFramer *Framer

	remoteFlowController *FlowController
	originFlowController *FlowController
}

func (fd *FrameDumper) Dump(chunk []byte, remote bool) {
	callback := func(frame http2.Frame) error {
		fd.DumpFrameHeader(frame, remote)

		switch frame := frame.(type) {
		case *http2.DataFrame:
			fd.DumpDataFrame(frame, remote)
		case *http2.HeadersFrame:
			fd.DumpHeadersFrame(frame, remote)
		case *http2.PriorityFrame:
			fd.DumpPriorityFrame(frame, remote)
		case *http2.RSTStreamFrame:
			fd.DumpRSTStreamFrame(frame, remote)
		case *http2.SettingsFrame:
			fd.DumpSettingsFrame(frame, remote)
		case *http2.PushPromiseFrame:
			fd.DumpPushPromiseFrame(frame, remote)
		case *http2.PingFrame:
			fd.DumpPingFrame(frame, remote)
		case *http2.GoAwayFrame:
			fd.DumpGoAwayFrame(frame, remote)
		case *http2.WindowUpdateFrame:
			fd.DumpWindowUpdateFrame(frame, remote)
		case *http2.ContinuationFrame:
			fd.DumpContinuationFrame(frame, remote)
		}

		return nil
	}

	if remote {
		fd.remoteFramer.ReadFrame(chunk, callback)
	} else {
		fd.originFramer.ReadFrame(chunk, callback)
	}
}

func (fd *FrameDumper) DumpFrameHeader(frame http2.Frame, remote bool) {
	header := frame.Header()
	frameType := header.Type.String()
	frameFlags := header.Flags

	if remote {
		frameType = color("cyan", frameType)
	} else {
		frameType = color("magenta", frameType)
	}

	msg := "%s Frame <Length:%d, Flags:0x%x>"
	logger.LogFrame(remote, fd.ConnectionID, header.StreamID, msg, frameType, header.Length, frameFlags)

	if frameFlags > 0 {
		logger.LogFrameInfo("Flags:")
		candidateFlags := flagName[header.Type]
		for f, n := range candidateFlags {
			if (f & frameFlags) != 0 {
				logger.LogFrameInfo("  %s (0x%x)", n, f)
			}
		}

	}
}

func (fd *FrameDumper) DumpDataFrame(frame *http2.DataFrame, remote bool) {
	var currentWindowSize, delta int32

	size := int32(len(frame.Data()))
	streamID := frame.Header().StreamID

	var fc *FlowController
	if remote {
		fc = fd.originFlowController
	} else {
		fc = fd.remoteFlowController
	}

	logger.LogFrameInfo("Window Size:")
	currentWindowSize, delta = fc.UpdateConnectionWindow(-size)
	logger.LogFrameInfo("  Connection: %d (%d)", currentWindowSize, delta)
	currentWindowSize, delta = fc.UpdateStreamWindow(streamID, -size)
	logger.LogFrameInfo("  Stream: %d (%d)", currentWindowSize, delta)
}

func (fd *FrameDumper) DumpHeadersFrame(frame *http2.HeadersFrame, remote bool) {
	if frame.HasPriority() {
		priority := frame.Priority

		var exclusive string
		if priority.Exclusive {
			exclusive = "Yes"
		} else {
			exclusive = "No"
		}

		logger.LogFrameInfo("Stream Dependency: %d", priority.StreamDep)
		logger.LogFrameInfo("Weight: %d", priority.Weight)
		logger.LogFrameInfo("Exclusive: %s", exclusive)
	}

	hf := frame.HeaderBlockFragment()

	var f *Framer
	if remote {
		f = fd.remoteFramer
	} else {
		f = fd.originFramer
	}

	label := false
	f.ReadHeader(hf, func(header hpack.HeaderField) {
		if !label {
			logger.LogFrameInfo("Header Fields:")
			label = true
		}
		logger.LogFrameInfo("  %s: %s", header.Name, header.Value)
	})
}

func (fd *FrameDumper) DumpPriorityFrame(frame *http2.PriorityFrame, remote bool) {
	priority := frame.PriorityParam

	var exclusive string
	if priority.Exclusive {
		exclusive = "Yes"
	} else {
		exclusive = "No"
	}

	logger.LogFrameInfo("Stream Dependency: %d", priority.StreamDep)
	logger.LogFrameInfo("Weight: %d", priority.Weight)
	logger.LogFrameInfo("Exclusive: %s", exclusive)
}

func (fd *FrameDumper) DumpRSTStreamFrame(frame *http2.RSTStreamFrame, remote bool) {
	logger.LogFrameInfo("Error Code: %s (0x%d)", frame.ErrCode.String(), frame.ErrCode)
}

func (fd *FrameDumper) DumpSettingsFrame(frame *http2.SettingsFrame, remote bool) {
	if frame.IsAck() {
		return
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

	label := false
	frame.ForeachSetting(func(setting http2.Setting) error {
		if !label {
			logger.LogFrameInfo("Parameters:")
			label = true
		}
		logger.LogFrameInfo("  %s(0x%d): %d", setting.ID.String(), setting.ID, setting.Val)
		return nil
	})
}

func (fd *FrameDumper) DumpPushPromiseFrame(frame *http2.PushPromiseFrame, remote bool) {
	logger.LogFrameInfo("Promised Stream ID: %d", frame.PromiseID)

	hf := frame.HeaderBlockFragment()

	var f *Framer
	if remote {
		f = fd.remoteFramer
	} else {
		f = fd.originFramer
	}

	label := false
	f.ReadHeader(hf, func(header hpack.HeaderField) {
		if !label {
			logger.LogFrameInfo("Header Fields:")
			label = true
		}
		logger.LogFrameInfo("  %s: %s", header.Name, header.Value)
	})
}

func (fd *FrameDumper) DumpPingFrame(frame *http2.PingFrame, remote bool) {
	data := frame.Data
	if len(data) > 0 {
		logger.LogFrameInfo("Opaque Data: 0x%x", data)
	}
}

func (fd *FrameDumper) DumpGoAwayFrame(frame *http2.GoAwayFrame, remote bool) {
	logger.LogFrameInfo("Last Stream ID: %d", frame.LastStreamID)
	logger.LogFrameInfo("Error Code: %s (0x%d)", frame.ErrCode.String(), frame.ErrCode)

	debug := frame.DebugData()
	if len(debug) > 0 {
		logger.LogFrameInfo("Additional Debug Data: %s", debug)
	}
}

func (fd *FrameDumper) DumpWindowUpdateFrame(frame *http2.WindowUpdateFrame, remote bool) {
	size := int32(frame.Increment)
	logger.LogFrameInfo("Window Size Increment: %d", size)

	var fc *FlowController
	if remote {
		fc = fd.remoteFlowController
	} else {
		fc = fd.originFlowController
	}

	streamID := frame.Header().StreamID
	logger.LogFrameInfo("Window Size:")
	if streamID == 0 {
		currentWindowSize, delta := fc.UpdateConnectionWindow(size)
		logger.LogFrameInfo("  Connection: %d (+%d)", currentWindowSize, delta)
	} else {
		currentWindowSize, delta := fc.UpdateStreamWindow(streamID, size)
		logger.LogFrameInfo("  Stream: %d (+%d)", currentWindowSize, delta)
	}
}

func (fd *FrameDumper) DumpContinuationFrame(frame *http2.ContinuationFrame, remote bool) {
	hf := frame.HeaderBlockFragment()

	var f *Framer
	if remote {
		f = fd.remoteFramer
	} else {
		f = fd.originFramer
	}

	label := false
	f.ReadHeader(hf, func(header hpack.HeaderField) {
		if !label {
			logger.LogFrameInfo("Header Fields:")
			label = true
		}
		logger.LogFrameInfo("  %s: %s", header.Name, header.Value)
	})
}

func NewFrameDumper(id string) *FrameDumper {
	dumper := &FrameDumper{
		ConnectionID: id,

		remoteFramer: NewFramer(true),
		originFramer: NewFramer(false),

		remoteFlowController: NewFlowController(),
		originFlowController: NewFlowController(),
	}

	return dumper
}
