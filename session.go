package rtp

import (
	"net"
	"sync"
)

type Session struct {
	addr           net.Addr
	ssrc           uint32
	receive        chan *Packet
	processor      Processor
	mux            sync.RWMutex
	closed         chan bool
	lastActiveTime int64
}

func newSession(ssrc uint32, addr net.Addr) *Session {
	sess := &Session{
		ssrc:   ssrc,
		addr:   addr,
		closed: make(chan bool),
	}
	return sess
}

func (sess *Session) Addr() net.Addr {
	return sess.addr
}

func (sess *Session) SSRC() uint32 {
	return sess.ssrc
}

func (sess *Session) Attach(processor Processor) {
	sess.mux.Lock()
	old := sess.processor
	sess.processor = processor
	sess.mux.Unlock()
	if old != nil {
		old.Release()
	}
}

func (sess *Session) close() {
	select {
	case <-sess.closed:
	default:
		close(sess.closed)
	}
}

func (sess *Session) process() {
	for {
		select {
		case pkt := <-sess.receive:
			if sess.processor != nil {
				sess.processor.Process(pkt)
			}
		case <-sess.closed:
			sess.release()
			return
		}
	}
}

func (sess *Session) release() {
	sess.mux.Lock()
	processor := sess.processor
	sess.processor = nil
	sess.mux.Unlock()

	if processor != nil {
		processor.Release()
	}
}
