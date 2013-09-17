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
	"time"
)

var (
	listenAddr        *string = flag.String("l", "localhost:8081", `listening address`)
	responderAddr     *string = flag.String("r", "localhost:8080", `responder address`)
	forwardAddr       *string = flag.String("f", "localhost:8082", `forwarder address`)
	writeFiles        *bool   = flag.Bool("w", true, `write files`)
	responderStopOnRN *bool   = flag.Bool("x", true, `responder use \r\n as stop`)
	iniWriter         *bufio.Writer
	resWriter         *bufio.Writer
	fwdWriter         *bufio.Writer
)

const (
	bufferSize = 256
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

func teeConn(iniConn *net.TCPConn) {
	upstream := make(chan []byte)
	downstream := make(chan []byte)

	resConn, err := makeTCPConn(*responderAddr)
	if err != nil {
		log.Panicf("Failed to create TCP conn, err: ", err)
	}
	defer resConn.Close()

	fwdConn, err := makeTCPConn(*forwardAddr)
	if err != nil {
		log.Printf("Failed to create TCP conn, err: ", err)
	}
	defer fwdConn.Close()

	log.Printf("ini %v", iniConn)
	log.Printf("res %v", resConn)
	log.Printf("fwd %v", fwdConn)

	go writeDownstream(resConn, fwdConn, downstream)
	go writeInitiator(iniConn, upstream)

	go readForwarder(fwdConn)
	go readResponder(upstream, resConn)

	readInitiator(downstream, iniConn)
}

func readInitiator(downstream chan<- []byte, conn *net.TCPConn) {
	writer := iniWriter
	readConnFunc(conn, false, func(data []byte) {
		downstream <- data
		if writer != nil {
			writer.Write(data)
		}
	})
	if writer != nil {
		time.AfterFunc(5*time.Second, func() {
			writer.Flush()
		})
	}
}

func readResponder(upstream chan<- []byte, conn *net.TCPConn) {
	writer := resWriter
	readConnFunc(conn, *responderStopOnRN, func(data []byte) {
		upstream <- data
		if writer != nil {
			writer.Write(data)
		}
	})
	if writer != nil {
		time.AfterFunc(5*time.Second, func() {
			writer.Flush()
		})
	}
}

func readForwarder(conn *net.TCPConn) {
	// conn.SetReadDeadline(time.Now().Add(1000 * time.Millisecond))
	writer := fwdWriter
	readConnFunc(conn, *responderStopOnRN, func(data []byte) {
		if writer != nil {
			writer.Write(data)
		}
	})
	if writer != nil {
		time.AfterFunc(5*time.Second, func() {
			writer.Flush()
		})
	}
}

func writeDownstream(resConn *net.TCPConn, fwdConn *net.TCPConn, downstream <-chan []byte) {
	// fwdConn.SetWriteDeadline(time.Now().Add(1000 * time.Millisecond))
	for data := range downstream {
		if _, err := resConn.Write(data); err != nil {
			log.Panicf("Failed to stream to conn, err:", err)
		}
		if _, err := fwdConn.Write(data); err != nil {
			log.Printf("Failed to stream to conn, err:", err)
		}
	}
}

func writeInitiator(conn *net.TCPConn, upstream <-chan []byte) {
	for data := range upstream {
		if _, err := conn.Write(data); err != nil {
			log.Panicf("Failed to stream to conn, err:", err)
		}
	}
}

func newFileAndWriter(filename string) (*os.File, *bufio.Writer, error) {
	fo, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, err
	}
	writer := bufio.NewWriter(fo)
	return fo, writer, nil
}

func readConnFunc(conn *net.TCPConn, stopOnRN bool, fn func([]byte)) {
	buffer := make([]byte, bufferSize)
	for {
		n, err := conn.Read(buffer)
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
		copy(fnData, buffer[:n])
		fn(fnData[:n])

		if err == io.EOF {
			log.Println("stop reading conn, EOF %v", conn)
			break
		}

		if stopOnRN && buffer[n-2] == '\r' && buffer[n-1] == '\n' {
			log.Printf("broke using islast")
			break
		}
	}

	log.Printf("finished reading from %v", conn)
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
	fmt.Printf("Forwarder: %v\n", *forwardAddr)

	if *writeFiles {
		rf, rw, err := newFileAndWriter("responder.log")
		if err != nil {
			log.Panicf("Failed to create file, err", err)
		}
		resWriter = rw
		defer rf.Close()
		ff, fw, err := newFileAndWriter("forwarder.log")
		if err != nil {
			log.Panicf("Failed to create file, err", err)
		}
		fwdWriter = fw
		defer ff.Close()
		lf, lw, err := newFileAndWriter("listener.log")
		if err != nil {
			log.Panicf("Failed to create file, err", err)
		}
		iniWriter = lw
		defer lf.Close()
	}

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
