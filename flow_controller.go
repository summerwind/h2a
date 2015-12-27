package main

type FlowController struct {
	InitialWindowSize    uint32
	ConnectionWindowSize int32
	StreamWindowSize     map[uint32]int32
}

func (fc *FlowController) UpdateConnectionWindow(size int32) (int32, int32) {
	original := fc.ConnectionWindowSize

	fc.ConnectionWindowSize += size
	if fc.ConnectionWindowSize < 0 {
		fc.ConnectionWindowSize = 0
	}

	return fc.ConnectionWindowSize, (fc.ConnectionWindowSize - original)
}

func (fc *FlowController) UpdateStreamWindow(streamID uint32, size int32) (int32, int32) {
	_, ok := fc.StreamWindowSize[streamID]
	if !ok {
		fc.StreamWindowSize[streamID] = int32(fc.InitialWindowSize)
	}

	original := fc.StreamWindowSize[streamID]

	fc.StreamWindowSize[streamID] += size
	if fc.StreamWindowSize[streamID] < 0 {
		fc.StreamWindowSize[streamID] = 0
	}

	return fc.StreamWindowSize[streamID], (fc.StreamWindowSize[streamID] - original)
}

func NewFlowController() *FlowController {
	initSize := 65535

	fc := &FlowController{
		InitialWindowSize:    uint32(initSize),
		ConnectionWindowSize: int32(initSize),
		StreamWindowSize:     map[uint32]int32{},
	}

	return fc
}
