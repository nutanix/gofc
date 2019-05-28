package gofc

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/nutanix/gofc/ofprotocol/ofp13"
)

type MockDispatcher struct {
	packetsHandled int
}

func (d *MockDispatcher) handleMessage(msg ofp13.OFMessage, dp *Datapath) {
	d.packetsHandled += 1
}

type DummyAddr struct {
}

func (d DummyAddr) String() string {
	return ""
}

func (d DummyAddr) Network() string {
	return ""
}

type MockConn struct {
	readBuf   *bytes.Buffer
	l         sync.Mutex
	c         *sync.Cond
	waitLen   int
	onWaitLen func()
	grp       sync.WaitGroup
}

func newMockConn() *MockConn {
	res := new(MockConn)
	res.c = sync.NewCond(&res.l)
	res.readBuf = &bytes.Buffer{}
	res.onWaitLen = func() {}
	res.waitLen = 0
	res.grp.Add(1)
	return res
}

func (c *MockConn) Read(b []byte) (n int, err error) {

	for {
		c.l.Lock()
		if c.readBuf == nil {
			c.l.Unlock()
			return 0, io.EOF
		}
		if c.readBuf.Len() == 0 {
			c.c.Wait()
			c.l.Unlock()
			continue
		}
		n, err = c.readBuf.Read(b)
		c.c.Broadcast()
		if c.readBuf.Len() == c.waitLen {
			c.l.Unlock()
			c.onWaitLen()
			c.l.Lock()
			c.grp.Done()
		}
		c.l.Unlock()
		return
	}
}

func (c *MockConn) Write(b []byte) (n int, err error) {
	c.l.Lock()
	if c.readBuf == nil {
		return 0, io.EOF
	}
	n, err = c.readBuf.Write(b)
	c.c.Broadcast()
	c.l.Unlock()
	return
}

func (c *MockConn) Close() error {
	c.l.Lock()
	if nil != c.readBuf {
		c.readBuf = nil
		c.c.Broadcast()
	}
	c.l.Unlock()
	return nil
}

func (c *MockConn) LocalAddr() net.Addr { return DummyAddr{} }

func (c *MockConn) RemoteAddr() net.Addr { return DummyAddr{} }

func (c *MockConn) SetDeadline(t time.Time) error { return errors.New("") }

func (c *MockConn) SetReadDeadline(t time.Time) error { return errors.New("") }

func (c *MockConn) SetWriteDeadline(t time.Time) error { return errors.New("") }

func TestDispatchManyPackets(t *testing.T) {
	packet := []byte{
		0x04,
		0x03,
		0x00, 0x08,
		0x00, 0x00, 0x00, 0x00,
	}
	packetsCount := 100
	c := newMockConn()
	d := MockDispatcher{}
	dp := NewDatapath(c, &d)
	for i := 0; i < packetsCount; i++ {
		if n, err := c.Write(packet); err != nil || len(packet) != n {
			t.Fail()
		}
	}
	dp.Start()
	c.grp.Wait()
	dp.Shutdown()
	if d.packetsHandled != packetsCount {
		t.Errorf("Wait %d, actual %d", packetsCount, d.packetsHandled)
	}
}

func TestShutdown(t *testing.T) {
	packet := []byte{
		0x04,
		0x03,
		0x00, 0x08,
		0x00, 0x00, 0x00, 0x00,
	}
	packetsCount := 100
	c := newMockConn()
	d := MockDispatcher{}
	dp := NewDatapath(c, &d)
	for i := 0; i < packetsCount; i++ {
		if n, err := c.Write(packet); err != nil || len(packet) != n {
			t.Fail()
		}
	}
	packetsCount = packetsCount / 2
	c.waitLen = len(packet) * packetsCount
	c.onWaitLen = dp.Shutdown
	dp.Start()
	c.grp.Wait()
	// last read was after shutdown
	if d.packetsHandled != packetsCount-1 {
		t.Errorf("Wait %d, actual %d", packetsCount, d.packetsHandled)
	}
}

func TestOnCloseCalledOnce(t *testing.T) {
	counter := 0
	counter2 := 0
	c := newMockConn()
	d := MockDispatcher{}
	dp := NewDatapath(c, &d)
	dp.RegisterOnClose(func(dp *Datapath) {
		counter += 1
	})
	dp.RegisterOnClose(func(dp *Datapath) {
		counter2 += 1
	})

	c.Close()
	dp.Close()
	dp.Close()
	if counter != 1 {
		t.Errorf("Invalid OnClose's call count")
	}
	if counter2 != 1 {
		t.Errorf("Invalid OnClose's call count")
	}
}

func TestReadErrorCausedClose(t *testing.T) {
	// test hangs if fails
	l := sync.Mutex{}
	cnd := sync.NewCond(&l)
	packet := []byte{
		0x04,
		0x03,
		0x00, 0x08,
		0x00, 0x00, 0x00, 0x00,
	}
	counter := 0
	c := newMockConn()
	d := MockDispatcher{}
	dp := NewDatapath(c, &d)
	dp.RegisterOnClose(func(dp *Datapath) {
		l.Lock()
		cnd.Signal()
		counter += 1
		l.Unlock()
	})
	if n, err := c.Write(packet); err != nil || len(packet) != n {
		t.Fail()
	}
	c.waitLen = len(packet) / 2
	c.onWaitLen = func() {
		c.Close()
	}
	dp.Start()
	c.grp.Wait()
	l.Lock()
	if counter != 1 {
		cnd.Wait()
	}
	if counter != 1 {
		t.Errorf("Invalid OnClose's call count")
	}
	l.Unlock()
}
