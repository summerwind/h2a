package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

const VERSION = "v0.0.2"

var logger = log.New(os.Stderr, "", 0)

type OriginConfig struct {
	Addr   string
	Direct bool
}

func main() {
	port := flag.Int("p", 0, "")
	ip := flag.String("i", "127.0.0.1", "")
	originPort := flag.Int("P", 0, "")
	originHost := flag.String("H", "", "")
	originDirect := flag.Bool("D", false, "")
	certPath := flag.String("c", "", "")
	keyPath := flag.String("k", "", "")
	logFormat := flag.String("l", "default", "")
	version := flag.Bool("version", false, "")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n\n", os.Args[0])
		fmt.Println("Options:")
		fmt.Println("  -p:        Port (Default: 443)")
		fmt.Println("  -i:        IP Address (Default: 127.0.0.1)")
		fmt.Println("  -P:        Origin port")
		fmt.Println("  -H:        Origin host")
		fmt.Println("  -D:        Use HTTP/2 direct mode to connect origin")
		fmt.Println("  -c:        Certificate file")
		fmt.Println("  -k:        Certificate key file")
		fmt.Println("  -l:        Log format (default or json, Default: default)")
		fmt.Println("  --version: Display version information and exit.")
		fmt.Println("  --help:    Display this help and exit.")
		os.Exit(1)
	}

	flag.Parse()

	if *version {
		logger.Printf("h2a %s\n", VERSION)
		os.Exit(0)
	}

	if *port == 0 {
		*port = 443
	}
	addr := fmt.Sprintf("%s:%d", *ip, *port)

	if *originPort == 0 {
		logger.Fatalln("Origin port is not specified")
	}
	if *originHost == "" {
		logger.Fatalln("Origin host is not specified")
	}
	originConfig := OriginConfig{
		Addr:   fmt.Sprintf("%s:%d", *originHost, *originPort),
		Direct: *originDirect,
	}

	var formatter Formatter
	if *logFormat == "json" {
		formatter = JSONFormatter
	} else {
		formatter = DefaultFormatter
	}

	cert, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		logger.Fatalln("Invalid certificate file")
	}

	config := &tls.Config{}
	config.Certificates = []tls.Certificate{cert}
	config.NextProtos = append(config.NextProtos, "h2", "h2-16", "h2-15", "h2-14")

	server, err := tls.Listen("tcp", addr, config)
	if err != nil {
		logger.Fatalf("Could not bind address - %s\n", addr)
	}

	defer server.Close()

	for {
		remoteConn, err := server.Accept()
		if err != nil {
			logger.Printf("Unable to accept: %s", err)
			continue
		}

		go handlePeer(remoteConn, originConfig, formatter)
	}
}

func handlePeer(remoteConn net.Conn, originConfig OriginConfig, formatter Formatter) {
	var originConn net.Conn
	var err error

	defer remoteConn.Close()

	dumper := NewFrameDumper(remoteConn.RemoteAddr(), formatter)
	defer dumper.Close()

	remoteCh, remoteErrCh := handleConnection(remoteConn)

	select {
	case chunk := <-remoteCh:
		connState := remoteConn.(*tls.Conn).ConnectionState()
		dumper.DumpConnectionState(connState)

		config := &tls.Config{}
		config.NextProtos = append(config.NextProtos, connState.NegotiatedProtocol)
		config.CipherSuites = []uint16{connState.CipherSuite}
		config.ServerName = connState.ServerName
		config.InsecureSkipVerify = true

		if originConfig.Direct {
			originConn, err = net.Dial("tcp", originConfig.Addr)
		} else {
			dialer := new(net.Dialer)
			originConn, err = tls.DialWithDialer(dialer, "tcp", originConfig.Addr, config)
		}
		if err != nil {
			logger.Printf("Unable to connect to the origin: %s", err)
			return
		}

		defer originConn.Close()

		_, err = originConn.Write(chunk)
		if err != nil {
			logger.Printf("Unable to write data to the origin: %s", err)
			return
		}

		dumper.DumpFrame(chunk, true)

	case err := <-remoteErrCh:
		if err != io.EOF {
			logger.Printf("Connection error: %s", err)
		}
		return
	}

	originCh, originErrCh := handleConnection(originConn)

	for {
		select {
		case chunk := <-remoteCh:
			_, err := originConn.Write(chunk)
			if err != nil {
				logger.Printf("Unable to write data to the origin: %s", err)
				return
			}
			dumper.DumpFrame(chunk, true)

		case err := <-remoteErrCh:
			if err != io.EOF {
				logger.Printf("Connection error: %s", err)
			}
			return

		case chunk := <-originCh:
			_, err := remoteConn.Write(chunk)
			if err != nil {
				logger.Printf("Unable to write data to the connection: %s", err)
				return
			}
			dumper.DumpFrame(chunk, false)

		case err := <-originErrCh:
			if err != io.EOF {
				logger.Printf("Origin error: %s", err)
			}
			return
		}
	}
}

func handleConnection(conn net.Conn) (<-chan []byte, <-chan error) {
	dataCh := make(chan []byte)
	errCh := make(chan error, 1)

	go func() {
		for {
			buf := make([]byte, 16384)

			n, err := conn.Read(buf)
			if err != nil {
				errCh <- err
				break
			}

			dataCh <- buf[:n]
		}
	}()

	return dataCh, errCh
}
