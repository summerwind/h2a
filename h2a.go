package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"flag"
	"fmt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"io"
	"net"
	"os"
	"strings"
)

const VERSION = "v0.0.1"

const frameHeaderLen = 9

type Peer struct {
	Conn net.Conn
	ID   int
}

func NewPeer(conn net.Conn, id int) *Peer {
	peer := &Peer{
		Conn: conn,
		ID:   id,
	}
	return peer
}

type FrameDumper struct {
	ConnectionID int

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

	if remote {
		frameType = color("cyan", frameType)
	} else {
		frameType = color("magenta", frameType)
	}

	msg := "%s Frame <Length:%d, Flags:0x%x>"
	logger.LogFrame(remote, fd.ConnectionID, header.StreamID, msg, frameType, header.Length, header.Flags)
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

func NewFrameDumper(id int) *FrameDumper {
	dumper := &FrameDumper{
		ConnectionID: id,

		remoteFramer: NewFramer(true),
		originFramer: NewFramer(false),

		remoteFlowController: NewFlowController(),
		originFlowController: NewFlowController(),
	}

	return dumper
}

type Framer struct {
	writeBuf   *bytes.Buffer
	readBuf    *bytes.Buffer
	chunkBuf   []byte
	framer     *http2.Framer
	decoder    *hpack.Decoder
	hfCallback func(hpack.HeaderField)
	preface    bool
}

func (f *Framer) ReadFrame(chunk []byte, callback func(http2.Frame) error) {
	if !f.preface {
		if string(chunk[:24]) == "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n" {
			f.preface = true
			chunk = chunk[24:]
		}
	}

	chunk = append(f.chunkBuf, chunk...)
	if len(chunk) < frameHeaderLen {
		f.chunkBuf = chunk
		return
	}

	n := 0
	for {
		payload := int(uint32(chunk[n])<<16 | uint32(chunk[n+1])<<8 | uint32(chunk[n+2]))
		last := (n + frameHeaderLen + payload)

		if last <= len(chunk) {
			n = last
		}

		if last >= len(chunk) {
			break
		}
	}

	f.chunkBuf = chunk[n:]
	f.readBuf.Write(chunk[:n])

	for {
		frame, err := f.framer.ReadFrame()
		if err != nil {
			if err != io.EOF {
				fmt.Printf("Read frame error: %s", err)
			}
			break
		} else {
			callback(frame)
		}
	}
}

func (f *Framer) ReadHeader(chunk []byte, callback func(hpack.HeaderField)) {
	f.hfCallback = callback
	f.decoder.Write(chunk)
	f.hfCallback = nil
}

func (f *Framer) SetMaxDynamicTableSize(size uint32) {
	f.decoder.SetMaxDynamicTableSize(size)
}

func NewFramer(remote bool) *Framer {
	writeBuf := bytes.NewBuffer(make([]byte, 0))
	readBuf := bytes.NewBuffer(make([]byte, 0))

	framer := &Framer{
		writeBuf: writeBuf,
		readBuf:  readBuf,
		chunkBuf: []byte{},
		framer:   http2.NewFramer(writeBuf, readBuf),
		preface:  !remote,
	}

	framer.decoder = hpack.NewDecoder(4096, func(hf hpack.HeaderField) {
		if framer.hfCallback != nil {
			framer.hfCallback(hf)
		}
	})

	return framer
}

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

type StreamLogger struct {
	indent string
}

func (sl *StreamLogger) LogFrame(remote bool, connID int, streamID uint32, format string, a ...interface{}) {
	var remoteStr string
	if remote {
		remoteStr = color("cyan", "=>")
	} else {
		remoteStr = color("magenta", "<=")
	}
	fmt.Fprintf(os.Stdout, "%s [%3d] [%3d] %s\n", remoteStr, connID, streamID, fmt.Sprintf(format, a...))
}

func (sl *StreamLogger) LogFrameInfo(format string, a ...interface{}) {
	delimiter := color("gray", "|")
	fmt.Fprintf(os.Stdout, "%s%s %s\n", sl.indent, delimiter, fmt.Sprintf(format, a...))
}

func NewStreamLogger() *StreamLogger {
	logger := &StreamLogger{
		indent: strings.Repeat(" ", 15),
	}

	return logger
}

var logger = NewStreamLogger()

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

func main() {
	conn_id := 0

	port := flag.Int("p", 0, "")
	ip := flag.String("i", "127.0.0.1", "")
	originPort := flag.Int("P", 0, "")
	originHost := flag.String("H", "", "")
	certPath := flag.String("c", "", "")
	keyPath := flag.String("k", "", "")
	version := flag.Bool("version", false, "")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n\n", os.Args[0])
		fmt.Println("Options:")
		fmt.Println("  -p:        Port. (Default: 443)")
		fmt.Println("  -i:        IP Address. (Default: 127.0.0.1)")
		fmt.Println("  -P:        Origin port.")
		fmt.Println("  -H:        Origin host.")
		fmt.Println("  -c:        Certificate file.")
		fmt.Println("  -k:        Certificate key file.")
		fmt.Println("  --version: Display version information and exit.")
		fmt.Println("  --help:    Display this help and exit.")
		os.Exit(1)
	}

	flag.Parse()

	if *version {
		fmt.Fprintf(os.Stderr, "h2analyzer %s\n", VERSION)
		os.Exit(0)
	}

	if *port == 0 {
		*port = 443
	}
	addr := fmt.Sprintf("%s:%d", *ip, *port)

	if *originPort == 0 {
		fmt.Fprintf(os.Stderr, "Origin port is not specified\n")
		os.Exit(1)
	}
	if *originHost == "" {
		fmt.Fprintf(os.Stderr, "Origin host is not specified\n")
		os.Exit(1)
	}
	origin := fmt.Sprintf("%s:%d", *originHost, *originPort)

	cert, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid certificate file\n")
		os.Exit(1)
	}

	config := &tls.Config{}
	config.Certificates = []tls.Certificate{cert}
	config.NextProtos = append(config.NextProtos, "h2-14", "h2-15", "h2-16", "h2")
	config.Rand = rand.Reader

	server, err := tls.Listen("tcp", addr, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not bind address - %s\n", addr)
		os.Exit(1)
	}

	defer server.Close()

	for {
		conn, err := server.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to accept: %s", err)
			continue
		}

		conn_id++
		remote := NewPeer(conn, conn_id)
		go handleConnection(remote, origin)
	}
}

func handleConnection(remote *Peer, originAddr string) {
	var dumper *FrameDumper
	var origin *Peer

	defer remote.Conn.Close()

	remoteCh, remoteErrCh := handlePeer(remote)

	select {
	case chunk := <-remoteCh:
		connState := remote.Conn.(*tls.Conn).ConnectionState()

		config := &tls.Config{}
		config.NextProtos = append(config.NextProtos, connState.NegotiatedProtocol)
		config.CipherSuites = []uint16{connState.CipherSuite}
		config.ServerName = connState.ServerName
		config.InsecureSkipVerify = true

		dialer := new(net.Dialer)
		conn, err := tls.DialWithDialer(dialer, "tcp", originAddr, config)
		if err != nil {
			logger.LogFrame(true, remote.ID, 0, "Unable to connect to the origin: %s", err)
			return
		}

		origin = NewPeer(conn, remote.ID)
		defer origin.Conn.Close()

		_, err = origin.Conn.Write(chunk)
		if err != nil {
			logger.LogFrame(true, remote.ID, 0, "Unable to proxy data: %s", err)
			return
		}

		dumper = NewFrameDumper(remote.ID)
		dumper.Dump(chunk, true)

	case err := <-remoteErrCh:
		if err == io.EOF {
			logger.LogFrame(true, remote.ID, 0, "Closed")
		} else {
			logger.LogFrame(true, remote.ID, 0, "Error: %s", err)
		}
		return
	}

	originCh, originErrCh := handlePeer(origin)

	for {
		select {
		case chunk := <-remoteCh:
			_, err := origin.Conn.Write(chunk)
			//fmt.Printf("Write to origin: %x byte\n", chunk)
			if err != nil {
				logger.LogFrame(true, remote.ID, 0, "Unable to proxy data: %s", err)
				return
			}
			dumper.Dump(chunk, true)

		case err := <-remoteErrCh:
			if err == io.EOF {
				logger.LogFrame(true, remote.ID, 0, "Closed")
			} else {
				logger.LogFrame(true, remote.ID, 0, "Error: %s", err)
			}
			return

		case chunk := <-originCh:
			_, err := remote.Conn.Write(chunk)
			//fmt.Printf("Write to remote: %x byte\n", chunk)
			if err != nil {
				logger.LogFrame(false, remote.ID, 0, "Unable to proxy data: %s", err)
				return
			}
			dumper.Dump(chunk, false)

		case err := <-originErrCh:
			if err == io.EOF {
				logger.LogFrame(false, remote.ID, 0, "Closed")
			} else {
				logger.LogFrame(false, remote.ID, 0, "Error: %s", err)
			}
			return
		}
	}
}

func handlePeer(peer *Peer) (<-chan []byte, <-chan error) {
	peerCh := make(chan []byte)
	peerErrCh := make(chan error, 1)

	go func() {
		for {
			buf := make([]byte, 16384)

			n, err := peer.Conn.Read(buf)
			//fmt.Printf("Read: %d byte\n", n)
			if err != nil {
				peerErrCh <- err
				break
			}

			peerCh <- buf[:n]
		}
	}()

	return peerCh, peerErrCh
}
