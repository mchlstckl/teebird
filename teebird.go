package main

import (
	"bufio"
	// "bytes"
	// "encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	// "time"
)

var (
	listenAddr        *string = flag.String("l", "localhost:8081", "listen address")
	responderAddr     *string = flag.String("r", "localhost:8080", "responder address")
	forwardAddr       *string = flag.String("f", "localhost:8082", "forwarding address")
	writeFiles        *bool   = flag.Bool("w", true, "write files")
	responderStopOnRN *bool   = flag.Bool("x", true, "responder use \\r\\n as stop")
)

type byteChanWriter chan<- []byte

func (w byteChanWriter) Write(p []byte) (int, error) {
	w <- p
	return len(p), nil
}

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
	reqChan := make(chan []byte)
	resChan := make(chan []byte)
	fwdChan := make(chan []byte)

	log.Printf("req %v", reqConn)

	go fanOut(reqChan, resChan, fwdChan)
	go forwardFlow(fwdChan)
	go handleRequestor(reqChan, reqConn)

	responderFlow(resChan, reqConn)
}

func responderFlow(readChan <-chan []byte, reqConn *net.TCPConn) {
	conn, err := makeTCPConn(*responderAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	log.Printf("res %v", conn)

	writeConnFromChan(conn, readChan, true)

	if *writeFiles {
		fo, err := os.OpenFile("responder.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		defer fo.Close()

		writer := bufio.NewWriter(fo)

		readConnToWriters(conn, *responderStopOnRN, reqConn, writer)

		log.Printf("time to flush res")

		if err = writer.Flush(); err != nil {
			log.Printf("Failed to flush %v, err: %v", writer, err)
		}
	} else {
		readConnToWriters(conn, *responderStopOnRN, reqConn)
	}
}

func forwardFlow(readChan <-chan []byte) {
	conn, err := makeTCPConn(*forwardAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	log.Printf("fwd %v", conn)

	// conn.SetWriteDeadline(time.Now().Add(1000 * time.Millisecond))
	writeConnFromChan(conn, readChan, false)

	if *writeFiles {
		fo, err := os.OpenFile("forward.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		defer fo.Close()

		writer := bufio.NewWriter(fo)

		// conn.SetReadDeadline(time.Now().Add(1000 * time.Millisecond))
		readConnToWriters(conn, *responderStopOnRN, writer)

		log.Printf("time to flush fwd")

		if err = writer.Flush(); err != nil {
			log.Printf("Failed to flush %v, err: %v", writer, err)
		}
	} else {
		readConnToWriters(conn, *responderStopOnRN)
	}
}

func readConnFunc(conn *net.TCPConn, stopOnRN bool, fn func([]byte)) {
	data := make([]byte, 256)
	for {
		n, err := conn.Read(data)
		if err != nil {
			if err != io.EOF {
				log.Printf("Failed to read %v from TCPConn with err: %v", conn, err)
				break
			}
		}

		if n < 1 {
			log.Printf("stop reading conn, n smaller than 1: %v", conn)
			break
		}

		fnData := make([]byte, n)
		copy(fnData, data[:n])
		fn(fnData[:n])

		if err == io.EOF {
			log.Println("stop reading conn, EOF %v", conn)
			break
		}

		if stopOnRN && data[n-2] == '\r' && data[n-1] == '\n' {
			log.Printf("broke using islast")
			break
		}
	}

	log.Printf("finished reading from %v", conn)
}

func readConnToWriters(srcConn *net.TCPConn, stopOnRN bool, writers ...io.Writer) {
	readConnFunc(srcConn, stopOnRN, func(data []byte) {
		for _, writer := range writers {
			writer.Write(data)
		}
	})
}

func handleRequestor(stream chan<- []byte, conn *net.TCPConn) {
	chanWriter := byteChanWriter(stream)
	if *writeFiles {
		fo, err := os.OpenFile("request.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		defer fo.Close()

		writer := bufio.NewWriter(fo)

		readConnToWriters(conn, true, chanWriter, writer)

		log.Printf("Done reading %v, closing stream", conn)

		if err = writer.Flush(); err != nil {
			log.Printf("Failed to flush %v, err: %v", writer, err)
		}
	} else {
		readConnToWriters(conn, true, chanWriter)
	}
	close(stream)
}

func fanOut(in <-chan []byte, outs ...chan<- []byte) {
	for data := range in {
		for _, out := range outs {
			out <- data
		}
	}

	log.Printf("Closing fan-out channs")

	for _, out := range outs {
		close(out)
	}
}

func writeConnFromChan(conn *net.TCPConn, stream <-chan []byte, panicOnErr bool) {
	log.Printf("Entering conn.write")
	for data := range stream {
		if _, err := conn.Write(data); err != nil && panicOnErr {
			log.Panicf("Failed to stream to conn, err:", err)
		}
	}
	log.Printf("Exiting conn.write")
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
