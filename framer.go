package main

import (
	"bytes"
	"fmt"
	"io"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
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

	cLen := len(chunk)

	if cLen < frameHeaderLen {
		f.chunkBuf = chunk
		return
	}

	pLen := int(uint32(chunk[0])<<16 | uint32(chunk[1])<<8 | uint32(chunk[2]))
	pEnd := (frameHeaderLen + pLen)

	if cLen < pEnd {
		f.chunkBuf = chunk
		return
	}

	f.readBuf.Write(chunk[:pEnd])
	f.chunkBuf = chunk[pEnd:]

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
