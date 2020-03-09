package gofc

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
)

type MockListener struct {
	acceptErr   error
	l           sync.Mutex
	c           *sync.Cond
	nextClients []*MockConn
}

func NewMockListener() *MockListener {
	res := &MockListener{}
	res.acceptErr = nil
	res.c = sync.NewCond(&res.l)
	res.nextClients = nil
	return res
}

func (l *MockListener) Accept() (c net.Conn, err error) {
	c = nil
	err = nil
	for {
		l.l.Lock()
		if len(l.nextClients) == 0 {
			if l.acceptErr != nil {
				err = l.acceptErr
				l.l.Unlock()
				return
			}
			l.c.Wait()
			l.l.Unlock()
			continue
		}
		break
	}
	if l.acceptErr != nil {
		err = l.acceptErr
		l.l.Unlock()
		return
	}
	c = l.nextClients[0]
	l.nextClients = l.nextClients[1:]
	l.l.Unlock()
	return
}

func (l *MockListener) NewClient() {
	l.l.Lock()
	l.nextClients = append(l.nextClients, newMockConn())
	l.c.Broadcast()
	l.l.Unlock()
}

func (l *MockListener) NewClientWithWait(waitLen int) {
	l.l.Lock()
	nextClient := newMockConn()
	nextClient.waitLen = waitLen
	l.nextClients = append(l.nextClients, nextClient)
	l.c.Broadcast()
	l.l.Unlock()
}

func (l *MockListener) Close() error {
	l.l.Lock()
	l.acceptErr = io.EOF
	l.c.Broadcast()
	l.l.Unlock()
	return nil
}

func (l *MockListener) Addr() net.Addr {
	return DummyAddr{}
}

func TestErrorListen(t *testing.T) {
	l := NewMockListener()
	l.acceptErr = net.UnknownNetworkError("")
	c := NewOFController()
	l.NewClient()
	err := c.Serve(l)
	switch err.(type) {
	case net.UnknownNetworkError:
		{
		}
	default:
		t.Errorf("Invalid error type %v", err)
	}
}

func TestCloseCausesErrServerClosed(t *testing.T) {
	l := NewMockListener()
	l.acceptErr = net.UnknownNetworkError("")
	c := NewOFController()
	l.NewClient()
	err := c.Serve(l)
	switch err.(type) {
	case net.UnknownNetworkError:
		{
		}
	default:
		t.Errorf("Invalid error type %v", err)
	}
}

func TestServeAfterClose(t *testing.T) {
	l := NewMockListener()
	c := NewOFController()
	err := c.Close()
	t.Logf("%v", err)
	if err := c.Serve(l); err != nil {
		if err != http.ErrServerClosed {
			t.Fail()
		}
	}
}

func TestServeCloseWithClient(t *testing.T) {
	l := NewMockListener()
	c := NewOFController()
	for i := 0; i < 1; i++ {
		l.NewClientWithWait(1000)
	}
	go func() {
		err := c.Close()
		if err != nil {
			t.Errorf("Error on close %v", err)
		}
	}()
	if err := c.Serve(l); err != nil {
		if err != http.ErrServerClosed {
			t.Fail()
		}
	}
}

func TestServeShutdownWithClient(t *testing.T) {
	l := NewMockListener()
	c := NewOFController()
	for i := 0; i < 1; i++ {
		l.NewClientWithWait(1000)
	}
	go func() {
		err := c.Shutdown(context.Background())
		if err != nil {
			t.Errorf("Error on close %v", err)
		}
	}()
	if err := c.Serve(l); err != nil {
		if err != http.ErrServerClosed {
			t.Fail()
		}
	}
}

func TestServeOnShutdownCalledOnce(t *testing.T) {
	counter := 0
	wg := sync.WaitGroup{}
	wg.Add(4)
	l := NewMockListener()
	c := NewOFController()
	c.RegisterOnShutdown(func() {
		counter += 1
		wg.Done()
	})
	for i := 0; i < 1; i++ {
		l.NewClientWithWait(1000)
	}
	go func() {
		err := c.Shutdown(context.Background())
		if err != nil {
			if err != http.ErrServerClosed {
				t.Errorf("Error on close %v", err)
			}
		}
		wg.Done()
	}()
	go func() {
		err := c.Shutdown(context.Background())
		if err != nil {
			if err != http.ErrServerClosed {
				t.Errorf("Error on close %v", err)
			}
		}
		wg.Done()
	}()
	go func() {
		err := c.Shutdown(context.Background())
		if err != nil {
			if err != http.ErrServerClosed {
				t.Errorf("Error on close %v", err)
			}
		}
		wg.Done()
	}()
	if err := c.Serve(l); err != nil {
		if err != http.ErrServerClosed {
			t.Fail()
		}
	}
	wg.Wait()
	if counter != 1 {
		t.Fail()
	}
}
