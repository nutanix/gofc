package gofc

import (
	"fmt"
	"net"
	"time"

	"github.com/nutanix/gofc/ofprotocol/ofp13"
)

var DEFAULT_PORT = 6653

/**
 * basic controller
 */
type OFController struct {
	echoInterval int32 // echo interval
}

func NewOFController() *OFController {
	ofc := new(OFController)
	ofc.echoInterval = 5
	return ofc
}

// func (c *OFController) HandleHello(msg *ofp13.OfpHello, dp *Datapath) {
// 	fmt.Println("recv Hello")
// 	// send feature request
// 	featureReq := ofp13.NewOfpFeaturesRequest()
// 	Send(dp, featureReq)
// }

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

func (c *OFController) ConnectionUp() {
	// handle connection up
}

func (c *OFController) ConnectionDown() {
	// handle connection down
}

func (c *OFController) sendEchoLoop(dp *Datapath) {
	// send echo request forever, first tick goes after
	// echo interval
	fmt.Println("Controller echo loop started for dp: %+v", dp)
	ticker := time.NewTicker(time.Duration(c.echoInterval) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		echoReq := ofp13.NewOfpEchoRequest()
		// ToDo: handle graceful shutdown for stale dps as this
		// go thread will never exit and may be hung on sendMessage
		// channel.
		dp.Send(echoReq)
	}
}

func ServerLoop(listenPort int) {
	var port int

	if listenPort <= 0 || listenPort >= 65536 {
		fmt.Println("Invalid port was specified. listen port must be between 0 - 65535.")
		return
	}
	port = listenPort

	tcpAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", port))
	listener, err := net.ListenTCP("tcp", tcpAddr)

	ofc := NewOFController()
	GetAppManager().RegistApplication(ofc)

	if err != nil {
		return
	}

	// wait for connect from switch
	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			return
		}
		go handleConnection(ofc, conn)
	}
}

/**
 *
 */
func handleConnection(c *OFController, conn *net.TCPConn) {
	// send hello
	hello := ofp13.NewOfpHello()
	_, err := conn.Write(hello.Serialize())
	if err != nil {
		fmt.Println(err)
	}

	// create datapath
	dp := NewDatapath(conn)

	// Start controller echo loop to keep
	// switch connection alive
	go c.sendEchoLoop(dp)

	// launch goroutine
	go dp.recvLoop()
	go dp.sendLoop()
}
