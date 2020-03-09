package gofc

import (
	"github.com/nutanix/gofc/ofprotocol/ofp13"
	"sync"
	"testing"
)

type MockOfController struct {
	counter int // for race detection
}

func (c *MockOfController) forEachHandler(f func(v interface{})) {
	f(c)
}

func (c *MockOfController) RegisterHandler(h interface{}) {

}

func (c *MockOfController) HandleEchoRequest(msg *ofp13.OfpHeader, dp *Datapath) {
	c.counter++
}

func TestHandleOneMessageAtOneMoment(t *testing.T) {
	group := sync.WaitGroup{}
	c := new(MockOfController)
	c.counter = 0
	d := NewDispatcher(c)
	echo := ofp13.NewOfpEchoRequest()
	echo.Type = 2
	echo.Version = 4
	echo.Xid = 18
	echo.Length = 8

	messagesCount := 1000
	group.Add(messagesCount)
	for i := 0; i < messagesCount; i++ {
		go func(group *sync.WaitGroup) {
			defer group.Done()
			d.handleMessage(echo, nil)
		}(&group)
	}
	group.Wait()
	if c.counter != messagesCount {
		t.Fail()
	}
}
