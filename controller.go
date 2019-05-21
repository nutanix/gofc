package gofc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/nutanix/gofc/ofprotocol/ofp13"
)

var DEFAULT_PORT = 6653

/**
 * basic controller
 */
type OFController struct {
	echoInterval int32 // echo interval
	dp           Datapath
	listener     net.Listener
	mtx          sync.Mutex
	onShutdown   []func()
	inShutdown   int32
	handlers     []interface{}
	dps          []*Datapath
	dispatcher   *Dispatcher
}

func NewOFController() *OFController {
	ofc := new(OFController)
	ofc.echoInterval = 60
	ofc.dispatcher = NewDispatcher(ofc)
	return ofc
}

func (c *OFController) HandleHello(msg *ofp13.OfpHello, dp *Datapath) {
	fmt.Println("recv Hello")
	// send feature request
	featureReq := ofp13.NewOfpFeaturesRequest()
	dp.Send(featureReq)
}

func (c *OFController) HandleSwitchFeatures(msg *ofp13.OfpSwitchFeatures, dp *Datapath) {
	fmt.Println("recv SwitchFeatures")
	// handle FeatureReply
	dp.datapathId = msg.DatapathId
}

func (c *OFController) HandleEchoRequest(msg *ofp13.OfpHeader, dp *Datapath) {
	fmt.Println("recv EchoReq")
	// send EchoReply
	echo := ofp13.NewOfpEchoReply()
	(*dp).Send(echo)
}

func (c *OFController) sendEchoLoop() {
	// TODO: send echo request forever
}

// Serve accepts incoming connections on the Listener l, creating a
// new service goroutines for each.
func (c *OFController) Serve(l net.Listener) error {
	if c.isShuttingDown() {
		return http.ErrServerClosed
	}

	c.listener = l
	for {
		conn, err := l.Accept()
		if err != nil {
			if c.isShuttingDown() {
				return http.ErrServerClosed
			}
			return err
		}
		go c.handleConnection(conn)
	}
}

func (c *OFController) onDpClosed(inDp *Datapath) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for i, dp := range c.dps {
		if dp == inDp {
			c.dps = append(c.dps[:i], c.dps[i+1:]...)
		}
	}
}

// ServerLoop listens on the network port and then
// calls Serve to handle requests on incoming connections.
func ServerLoop(listenPort int, h interface{}) {
	var port int

	if listenPort <= 0 || listenPort >= 65536 {
		fmt.Println("Invalid port was specified. listen port must be between 0 - 65535.")
		return
	}
	port = listenPort

	tcpAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("resolve tcp addr failed: %v", err)
		return
	}
	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		fmt.Printf("failed to listen: %v", err)
		return
	}
	ofc := NewOFController()
	ofc.RegisterHandler(h)
	if err := ofc.Serve(listener); err != nil {
		fmt.Printf("failed to serve: %v", err)
		return

	}
}

// Close all connections and the server.
func (c *OFController) Close() error {
	atomic.StoreInt32(&c.inShutdown, 1)
	c.mtx.Lock()
	defer c.mtx.Unlock()
	err := c.listener.Close()
	for _, dp := range c.dps {
		dp.Close()
	}
	return err
}

// Shutdown gracefully shuts down the server without interrupting any
// active connections
// Once Shutdown has been called on a server, it may not be reused;
// future calls to methods such as Serve will return ErrServerClosed.
func (c *OFController) Shutdown(ctx context.Context) error {
	atomic.StoreInt32(&c.inShutdown, 1)
	c.mtx.Lock()
	defer c.mtx.Unlock()
	err := c.listener.Close()
	for _, dp := range c.dps {
		dp.Shutdown()
	}
	for _, f := range c.onShutdown {
		go f()
	}
	return err
}

func (c *OFController) isShuttingDown() bool {
	return atomic.LoadInt32(&c.inShutdown) != 0
}

// RegisterOnShutdown registers a function to call on Shutdown.
func (c *OFController) RegisterOnShutdown(f func()) {
	c.mtx.Lock()
	c.onShutdown = append(c.onShutdown, f)
	c.mtx.Unlock()
}

func (c *OFController) RegisterHandler(h interface{}) {
	c.mtx.Lock()
	c.handlers = append(c.handlers, h)
	c.mtx.Unlock()
}

func (c *OFController) forEachHandler(f func(v interface{})) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	f(c)
	for _, h := range c.handlers {
		f(h)
	}
}

/**
 *
 */
func (c *OFController) handleConnection(conn net.Conn) {
	// send hello
	hello := ofp13.NewOfpHello()
	_, err := conn.Write(hello.Serialize())
	if err != nil {
		fmt.Println(err)
		return
	}

	// create datapath
	dp := NewDatapath(conn, c.dispatcher)
	c.mtx.Lock()
	c.dps = append(c.dps, dp)
	dp.RegisterOnClose(c.onDpClosed)
	c.mtx.Unlock()

	// launch goroutine
	go dp.recvLoop()
	go dp.sendLoop()
}
