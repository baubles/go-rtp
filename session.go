package rtp

import (
	"net"
	"sync"
	"time"
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
	attach             chan Processor
	srv                *Server
}

func newSession(ssrc uint32, addr net.Addr, srv *Server) *Session {
	sess := &Session{
		ssrc:    ssrc,
		addr:    addr,
		closed:  make(chan bool),
		errch:   make(chan error, 1),
		receive: make(chan *Packet, 100),
		attach:  make(chan Processor, 1),
		srv:     srv,
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
	} else {
		sess.attach <- processor
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
	select {
	case <-sess.attach:
	case <-sess.closed:
		return nil
	}
	lastPrintTime := time.Now().Unix()
	lost := uint16(0)
	duration := int64(60)
	for {
		select {
		case pkt := <-sess.receive:
			now := time.Now().Unix()
			if pkt.SequenceNumber-sess.lastSequenceNumber > 1 && now-lastPrintTime > 5 {
				lost = lost + pkt.SequenceNumber - sess.lastSequenceNumber
			}

			if now-lastPrintTime > duration && lost > 0 {
				logger.Printf("session ssrc=%d, loss %d packets in %ds, seq lastest : %d, current is %d\n", pkt.SSRC, pkt.SequenceNumber-sess.lastSequenceNumber-1, duration, sess.lastSequenceNumber, pkt.SequenceNumber)
				lastPrintTime = now
				lost = 0
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

func (sess *Session) Close() {
	sess.close()
	sess.srv.sessions.Delete(sess.ssrc)
}
