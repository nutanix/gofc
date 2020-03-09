package gofc

import (
	"context"
	"fmt"
	"github.com/nutanix/gofc/ofprotocol/ofp13"
	"net"
	"net/http"
	"sync"
)

var DefaultPort = 6653

type HandlerStorage interface {
	RegisterHandler(h interface{})
	forEachHandler(f func(v interface{}))
}

/**
 * basic controller
 */
type OFController struct {
	echoInterval int32 // echo interval
	dp           Datapath
	listener     net.Listener
	mtx          sync.Mutex
	onShutdown   []func()
	inShutdown   bool
	handlers     []interface{}
	dps          []*Datapath
	dispatcher   IDispatcher
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
	c.mtx.Lock()
	if c.inShutdown {
		c.mtx.Unlock()
		return http.ErrServerClosed
	}

	c.listener = l
	c.mtx.Unlock()
	for {
		conn, err := l.Accept()
		c.mtx.Lock()
		if err != nil {
			if c.inShutdown {
				c.mtx.Unlock()
				return http.ErrServerClosed
			}
			c.mtx.Unlock()
			return err
		}
		c.mtx.Unlock()
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
func (c *OFController) Close() (err error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.inShutdown = true
	if c.listener != nil {
		err = c.listener.Close()
	}
	for _, dp := range c.dps {
		c.mtx.Unlock()
		dp.Close()
		c.mtx.Lock()
	}
	return
}

// Shutdown gracefully shuts down the server without interrupting any
// active connections
// Once Shutdown has been called on a server, it may not be reused;
// future calls to methods such as Serve will return ErrServerClosed.
func (c *OFController) Shutdown(ctx context.Context) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.inShutdown {
		return http.ErrServerClosed
	}
	if c.listener == nil {
		return nil
	}
	c.inShutdown = true
	err := c.listener.Close()
	for _, dp := range c.dps {
		c.mtx.Unlock()
		dp.Shutdown()
		c.mtx.Lock()
	}
	for _, f := range c.onShutdown {
		go f()
	}
	return err
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
	dp.Start()
}
