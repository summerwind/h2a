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
	logger.LogFrame(true, remote.ID, 0, "Connected")

	select {
	case chunk := <-remoteCh:
		connState := remote.Conn.(*tls.Conn).ConnectionState()

		config := &tls.Config{}
		config.NextProtos = append(config.NextProtos, connState.NegotiatedProtocol)
		config.CipherSuites = []uint16{connState.CipherSuite}
		config.ServerName = connState.ServerName
		config.InsecureSkipVerify = true

		logger.LogFrame(true, remote.ID, 0, "Negotiated Protocol: %s", connState.NegotiatedProtocol)

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
