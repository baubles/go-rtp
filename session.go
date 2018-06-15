package rtp

import (
	"net"
	"sync"
)

type Session struct {
	addr               net.Addr
	ssrc               uint32
	receive            chan *Packet
	processor          Processor
	mux                sync.RWMutex
	closed             chan bool
	lastActiveTime     int64
	errch              chan error
	lastSequenceNumber uint16
}

func newSession(ssrc uint32, addr net.Addr) *Session {
	sess := &Session{
		ssrc:    ssrc,
		addr:    addr,
		closed:  make(chan bool),
		errch:   make(chan error, 1),
		receive: make(chan *Packet, 20),
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

func (sess *Session) process() error {
	for {
		select {
		case pkt := <-sess.receive:
			if pkt.SequenceNumber-sess.lastSequenceNumber > 1 {
				logger.Printf("session loss %d packets, SequenceNumber lastest : %d, current is %d\n", pkt.SequenceNumber-sess.lastSequenceNumber-1, sess.lastSequenceNumber, pkt.SequenceNumber)
			}

			if sess.processor != nil {
				err := sess.processor.Process(pkt)
				if err != nil {
					logger.Println("session process err", err)
					sess.errch <- err
					return err
				}
			}

			sess.lastSequenceNumber = pkt.SequenceNumber

		case <-sess.closed:
			sess.release()
			return nil
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

func (sess *Session) Wait() error {
	select {
	case <-sess.closed:
		return nil
	case err := <-sess.errch:
		return err
	}

}
