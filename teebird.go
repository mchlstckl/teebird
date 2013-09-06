package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"runtime"
	"time"
)

var (
	listenAddr    *string = flag.String("l", "localhost:8081", "listen address")
	responderAddr *string = flag.String("r", "localhost:8080", "responder address")
	forwardAddr   *string = flag.String("f", "localhost:8082", "forwarding address")
)

func closeConn(completed <-chan *net.TCPConn) {
	for conn := range completed {
		conn.Close()
	}
}

func makeTCPConn(addr string) (*net.TCPConn, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}
	return net.DialTCP("tcp", nil, tcpAddr)
}

func teeConn(reqConn *net.TCPConn) {
	resConn, err := makeTCPConn(*responderAddr)
	if err != nil {
		panic(err)
	}
	defer resConn.Close()

	buf := &bytes.Buffer{}
	readToBuffer(reqConn, buf)

	log.Printf("Read from requester: \n%v", hex.Dump(buf.Bytes()))

	// write to responder
	if _, err := resConn.Write(buf.Bytes()); err != nil {
		log.Panicf("Failed to write to responder with err: %v", err)
	}

	fBytes := make([]byte, buf.Len())
	copy(fBytes, buf.Bytes())

	go forwardBytes(fBytes)

	buf.Reset()
	readToBuffer(resConn, buf)

	log.Printf("Received from responder: \n%v", hex.Dump(buf.Bytes()))

	// send responder bytes to requester
	if _, err := reqConn.Write(buf.Bytes()); err != nil {
		log.Panicf("Failed to write to requester with err: %v", err)
	}
}

func readToBuffer(conn *net.TCPConn, buf *bytes.Buffer) {
	data := make([]byte, 256)
	for {
		n, err := conn.Read(data)
		if err != nil {
			if err != io.EOF {
				log.Panicf("Failed to read from TCPConn with err: %v", err)
			} else {
				break
			}
		}
		buf.Write(data[:n])
		if data[n-2] == '\r' && data[n-1] == '\n' {
			break
		}
	}
}

func forwardBytes(out []byte) {
	conn, _ := makeTCPConn(*forwardAddr)
	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// write to forward
	if _, err := conn.Write(out); err != nil {
		log.Printf("Forwarding failed with err: %v", err)
		return
	}

	log.Printf("Forwarded: \n%v", hex.Dump(out))

	// throw away response from forward receiver
	data := make([]byte, 1024)
	for {
		_, err := conn.Read(data)
		if err != nil {
			if err != io.EOF {
				log.Printf("Reading from forward address failed with err: %v", err)
				break
			} else {
				break
			}
		}
	}
}

func handleIncoming(incoming <-chan *net.TCPConn, completed chan<- *net.TCPConn) {
	for conn := range incoming {
		teeConn(conn)
		completed <- conn
	}
}

func main() {

	flag.Parse()

	fmt.Printf("Listening: %v\n", *listenAddr)
	fmt.Printf("Responder: %v\n", *responderAddr)
	fmt.Printf("Forward: %v\n", *forwardAddr)

	// setup listener
	addr, err := net.ResolveTCPAddr("tcp", *listenAddr)
	if err != nil {
		log.Panicf("Resolving listen address failed with err: %v", err)
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Panicf("Listen failed with err: %v", err)
	}

	incoming := make(chan *net.TCPConn)
	completed := make(chan *net.TCPConn)

	numHandlers := runtime.NumCPU()*2 + 1

	for i := 0; i < numHandlers; i++ {
		go handleIncoming(incoming, completed)
	}

	go closeConn(completed)

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			log.Panicf("Accept incoming failed with err: %v", err)
		}
		incoming <- conn
	}
}
