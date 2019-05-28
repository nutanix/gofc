package gofc

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/nutanix/gofc/ofprotocol/ofp13"
)

// datapath
type Datapath struct {
	done       chan bool
	conn       net.Conn
	datapathId uint64
	sendBuffer chan *ofp13.OFMessage
	readBuffer chan []byte
	ofpversion string
	ports      int
	inShutdown int32
	dispatcher IDispatcher
	onClose    []func(dp *Datapath)
	lock       sync.Mutex
}

/**
 * ctor
 */
func NewDatapath(conn net.Conn, d IDispatcher) *Datapath {
	dp := new(Datapath)
	dp.sendBuffer = make(chan *ofp13.OFMessage, 10)
	dp.readBuffer = make(chan []byte, 10)
	dp.done = make(chan bool, 2)
	dp.conn = conn
	dp.dispatcher = d
	return dp
}

func (dp *Datapath) isShuttingDown() bool { return dp.inShutdown == 1 }

func (dp *Datapath) RegisterOnClose(f func(dp *Datapath)) {
	dp.lock.Lock()
	defer dp.lock.Unlock()
	if dp.isShuttingDown() {
		return
	}
	dp.onClose = append(dp.onClose, f)
}

func (dp *Datapath) Start() {
	go dp.recvLoop()
	go dp.sendLoop()
}

func (dp *Datapath) Close() {
	dp.lock.Lock()
	if dp.isShuttingDown() {
		return
	}
	dp.inShutdown = 1
	if err := dp.conn.Close(); err != nil {
		fmt.Printf("%v", err)
	}
	for len(dp.sendBuffer) > 0 {
		<-dp.sendBuffer
	}
	close(dp.sendBuffer)
	for len(dp.readBuffer) > 0 {
		<-dp.readBuffer
	}
	close(dp.readBuffer)
	dp.lock.Unlock()

	for _, f := range dp.onClose {
		f(dp)
	}
}

func (dp *Datapath) Shutdown() {
	dp.lock.Lock()
	if dp.isShuttingDown() {
		return
	}
	dp.inShutdown = 1
	dp.lock.Unlock()
	close(dp.readBuffer)
	<-dp.done
	close(dp.sendBuffer)
	<-dp.done
	close(dp.done)
	if err := dp.conn.Close(); err != nil {
		fmt.Printf("%v", err)
	}
	for _, f := range dp.onClose {
		f(dp)
	}
}

func (dp *Datapath) sendLoop() {
	defer func() { dp.done <- true }()
	for {
		// wait channel
		msg, more := <-(dp.sendBuffer)
		// serialize data
		if !more {
			return
		}
		byteData := (*msg).Serialize()
		_, err := dp.conn.Write(byteData)
		if err != nil {
			fmt.Println("failed to write conn")
			fmt.Println(err)
			dp.Close()
			return
		}
	}
}

func (dp *Datapath) parseLoop() {
	defer func() { dp.done <- true }()
	for {
		buf, more := <-dp.readBuffer
		if !more {
			return
		}
		dp.handlePacket(buf)
	}
}

func (dp *Datapath) recvLoop() {
	go dp.parseLoop()
	// for more information see https://www.opennetworking.org/wp-content/uploads/2014/10/openflow-spec-v1.3.0.pdf
	const kHeaderSize = 8
	buf := make([]byte, 1024*64)
	for {
		dp.lock.Lock()
		if dp.isShuttingDown() {
			return
		}
		dp.lock.Unlock()
		for i := 0; i < kHeaderSize; {
			b, err := dp.conn.Read(buf[i : i+1]) // read len prefix and len
			if err != nil {
				dp.lock.Lock()
				if !dp.isShuttingDown() {
					fmt.Println("failed to read conn")
					fmt.Println(err)
					dp.lock.Unlock()
					dp.Close()
					return
				}
				dp.lock.Unlock()
				return
			}
			i += b
		}
		msgLen := (int)(binary.BigEndian.Uint16(buf[2:]))
		bufPos := kHeaderSize
		for bufPos < msgLen {
			read, err := dp.conn.Read(buf[bufPos:msgLen])
			if err != nil {
				dp.lock.Lock()
				if !dp.isShuttingDown() {
					fmt.Println("failed to read conn")
					fmt.Println(err)
					dp.lock.Unlock()
					dp.Close()
					return
				}
				dp.lock.Unlock()
				return
			}
			bufPos += read
		}
		if bufPos != msgLen {
			fmt.Printf("Strange ofp packet, len in packet %d, received len %d\n", msgLen, bufPos)
		}
		packet := make([]byte, msgLen)
		copy(packet, buf[0:msgLen])
		dp.lock.Lock()
		if !dp.isShuttingDown() {
			dp.readBuffer <- packet
		}
		dp.lock.Unlock()
	}
}

func (dp *Datapath) handlePacket(buf []byte) {
	// parse data
	msg := ofp13.Parse(buf[0:])

	if _, ok := msg.(*ofp13.OfpHello); ok {
		// handle hello
		featureReq := ofp13.NewOfpFeaturesRequest()
		dp.Send(featureReq)
	} else {
		// dispatch handler
		dp.dispatcher.handleMessage(msg, dp)
	}
}

/**
 *
 */
func (dp *Datapath) Send(message ofp13.OFMessage) bool {
	dp.lock.Lock()
	if dp.isShuttingDown() {
		return false
	}
	dp.lock.Unlock()
	// push data
	dp.sendBuffer <- &message
	return true
}

func (dp *Datapath) GetRemoteIp() string {
	ip := dp.conn.RemoteAddr().String()
	return ip[:strings.IndexByte(ip, ':')]
}

func (dp *Datapath) GetLocalIp() string {
	ip := dp.conn.LocalAddr().String()
	return ip[:strings.IndexByte(ip, ':')]
}
