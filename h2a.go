package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
)

const VERSION = "v0.0.2"

func main() {
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

	server, err := tls.Listen("tcp", addr, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not bind address - %s\n", addr)
		os.Exit(1)
	}

	defer server.Close()

	for {
		remoteConn, err := server.Accept()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to accept: %s", err)
			continue
		}

		go handlePeer(remoteConn, origin)
	}
}

func handlePeer(remoteConn net.Conn, originAddr string) {
	var originConn net.Conn
	var err error

	defer remoteConn.Close()

	dumper := NewFrameDumper(remoteConn.RemoteAddr())
	defer dumper.Close()

	remoteID := remoteConn.RemoteAddr().String()

	remoteCh, remoteErrCh := handleConnection(remoteConn)

	select {
	case chunk := <-remoteCh:
		connState := remoteConn.(*tls.Conn).ConnectionState()
		dumper.ConnectionState(connState)

		config := &tls.Config{}
		config.NextProtos = append(config.NextProtos, connState.NegotiatedProtocol)
		config.CipherSuites = []uint16{connState.CipherSuite}
		config.ServerName = connState.ServerName
		config.InsecureSkipVerify = true

		dialer := new(net.Dialer)
		originConn, err = tls.DialWithDialer(dialer, "tcp", originAddr, config)
		if err != nil {
			logger.LogFrame(true, remoteID, 0, "Unable to connect to the origin: %s", err)
			return
		}

		defer originConn.Close()

		_, err = originConn.Write(chunk)
		if err != nil {
			logger.LogFrame(true, remoteID, 0, "Unable to proxy data: %s", err)
			return
		}

		dumper.Dump(chunk, true)

	case err := <-remoteErrCh:
		if err != io.EOF {
			logger.LogFrame(true, remoteID, 0, "Error: %s", err)
		}
		return
	}

	originCh, originErrCh := handleConnection(originConn)

	for {
		select {
		case chunk := <-remoteCh:
			_, err := originConn.Write(chunk)
			//fmt.Printf("Write to origin: %x byte\n", chunk)
			if err != nil {
				logger.LogFrame(true, remoteID, 0, "Unable to proxy data: %s", err)
				return
			}
			dumper.Dump(chunk, true)

		case err := <-remoteErrCh:
			if err != io.EOF {
				logger.LogFrame(true, remoteID, 0, "Error: %s", err)
			}
			return

		case chunk := <-originCh:
			_, err := remoteConn.Write(chunk)
			//fmt.Printf("Write to remote: %x byte\n", chunk)
			if err != nil {
				logger.LogFrame(false, remoteID, 0, "Unable to proxy data: %s", err)
				return
			}
			dumper.Dump(chunk, false)

		case err := <-originErrCh:
			if err != io.EOF {
				logger.LogFrame(false, remoteID, 0, "Error: %s", err)
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
			//fmt.Printf("Read: %d byte\n", n)
			if err != nil {
				errCh <- err
				break
			}

			dataCh <- buf[:n]
		}
	}()

	return dataCh, errCh
}
