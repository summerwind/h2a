package main

import (
	"bytes"
	"fmt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"io"
)

const frameHeaderLen = 9

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
