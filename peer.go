package main

import (
	"net"
)

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
