package main

import (
	"net"
)

type Peer struct {
	Conn net.Conn
	ID   string
}

func NewPeer(conn net.Conn, ID string) *Peer {
	peer := &Peer{
		Conn: conn,
		ID:   ID,
	}
	return peer
}
