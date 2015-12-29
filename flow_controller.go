package main

import (
	"fmt"
)

type WindowSize struct {
	current uint32
	delta   int32
}

func (ws WindowSize) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%d", ws.current)), nil
}

type FlowController struct {
	InitialWindowSize    uint32
	ConnectionWindowSize uint32
	StreamWindowSize     map[uint32]uint32
}

func (fc *FlowController) UpdateConnectionWindow(size uint32) WindowSize {
	original := fc.ConnectionWindowSize

	fc.ConnectionWindowSize += size
	if fc.ConnectionWindowSize < 0 {
		fc.ConnectionWindowSize = 0
	}

	return WindowSize{
		fc.ConnectionWindowSize,
		int32(fc.ConnectionWindowSize - original),
	}
}

func (fc *FlowController) UpdateStreamWindow(streamID uint32, size uint32) WindowSize {
	_, ok := fc.StreamWindowSize[streamID]
	if !ok {
		fc.StreamWindowSize[streamID] = fc.InitialWindowSize
	}

	original := fc.StreamWindowSize[streamID]

	fc.StreamWindowSize[streamID] += size
	if fc.StreamWindowSize[streamID] < 0 {
		fc.StreamWindowSize[streamID] = 0
	}

	return WindowSize{
		fc.StreamWindowSize[streamID],
		int32(fc.StreamWindowSize[streamID] - original),
	}
}

func NewFlowController() *FlowController {
	initSize := 65535

	fc := &FlowController{
		InitialWindowSize:    uint32(initSize),
		ConnectionWindowSize: uint32(initSize),
		StreamWindowSize:     map[uint32]uint32{},
	}

	return fc
}
