package main

import (
	"context"
	"fmt"
	"github.com/nutanix/gofc"
	"github.com/nutanix/gofc/ofprotocol/ofp13"
	"net"
	"os"
	"os/signal"
	"time"
)

type SampleController struct {
	// add any paramter used in controller.
}

func NewSampleController() *SampleController {
	ofc := new(SampleController)
	return ofc
}

func (c *SampleController) HandleSwitchFeatures(msg *ofp13.OfpSwitchFeatures, dp *gofc.Datapath) {
	// create match
	ethdst, _ := ofp13.NewOxmEthDst("00:00:00:00:00:00")
	if ethdst == nil {
		fmt.Println(ethdst)
		return
	}
	match := ofp13.NewOfpMatch()
	match.Append(ethdst)

	// create Instruction
	instruction := ofp13.NewOfpInstructionActions(ofp13.OFPIT_APPLY_ACTIONS)

	// create actions
	seteth, _ := ofp13.NewOxmEthDst("11:22:33:44:55:66")
	instruction.Append(ofp13.NewOfpActionSetField(seteth))

	// append Instruction
	instructions := make([]ofp13.OfpInstruction, 0)
	instructions = append(instructions, instruction)

	// create flow mod
	fm := ofp13.NewOfpFlowModModify(
		0, // cookie
		0, // cookie mask
		0, // tableid
		0, // priority
		ofp13.OFPFF_SEND_FLOW_REM,
		match,
		instructions,
	)

	// send FlowMod
	dp.Send(fm)

	// Create and send AggregateStatsRequest
	mf := ofp13.NewOfpMatch()
	mf.Append(ethdst)
	mp := ofp13.NewOfpAggregateStatsRequest(0, 0, ofp13.OFPP_ANY, ofp13.OFPG_ANY, 0, 0, mf)
	dp.Send(mp)
}

func (c *SampleController) HandleAggregateStatsReply(msg *ofp13.OfpMultipartReply, dp *gofc.Datapath) {
	fmt.Println("Handle AggregateStats")
	for _, mp := range msg.Body {
		if obj, ok := mp.(*ofp13.OfpAggregateStats); ok {
			fmt.Println(obj.PacketCount)
			fmt.Println(obj.ByteCount)
			fmt.Println(obj.FlowCount)
		}
	}
}

func main() {
	// register app
	controller := gofc.NewOFController()
	controller.RegisterHandler(NewSampleController())

	// start server

	l, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: gofc.DEFAULT_PORT + 1})
	if err != nil {
		panic(err)
	}
	go func() {
		if err := controller.Serve(l); err != nil {
			panic(err)
		}
	}()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs)
	go func() {
		_ = <-sigs
		err = controller.Shutdown(context.Background())
		os.Exit(0)
	}()
	for {
		time.Sleep(time.Minute)
	}
}
