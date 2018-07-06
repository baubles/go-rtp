package rtp

import (
	"net"
	"runtime"
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
	srv                *Server

	buf       []*Packet
	bufCap    uint16
	bufOffset uint16
	bufLen    uint16

	tmpPkt *Packet

	timestamp uint32
	seq       uint16
}

var defaultSessionBufCap = uint16(200)

func newSession(ssrc uint32, addr net.Addr, srv *Server) *Session {
	sess := &Session{
		ssrc:    ssrc,
		addr:    addr,
		closed:  make(chan bool),
		errch:   make(chan error, 1),
		receive: make(chan *Packet, defaultSessionBufCap),
		srv:     srv,
		buf:     make([]*Packet, defaultSessionBufCap),
		bufCap:  defaultSessionBufCap,
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
	old := sess.processor
	sess.processor = processor
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
	lastPrintTime := time.Now().Unix()
	lost := uint16(0)
	duration := int64(60)
	for {
		pkt := sess.pull()
		if pkt == nil {
			return nil
		}

		now := time.Now().Unix()
		if pkt.SequenceNumber-sess.lastSequenceNumber > 1 && now-lastPrintTime > 5 {
			lost = lost + pkt.SequenceNumber - sess.lastSequenceNumber
		}

		if now-lastPrintTime > duration && lost > 0 {
			logger.Printf("session ssrc=%d, loss %d packets in %ds, current seq: %d\n", pkt.SSRC, lost, now-lastPrintTime, pkt.SequenceNumber)
			lastPrintTime = now
			lost = 0
		}

		if sess.processor != nil {
			err := sess.processor.Process(pkt)
			// logger.Printf("ssrc %d, seq %v, err %v\n", pkt.SSRC, pkt.SequenceNumber, err)
			pkt.release()
			if err != nil {
				logger.Println("session process err", err)
				sess.errch <- err
				return err
			}
		} else {
			pkt.release()
		}

		runtime.Gosched()
	}
}

func (sess *Session) pull() (pkt *Packet) {
	pkt = sess.readBuf()
	if pkt != nil {
		sess.seq++
		return pkt
	}

	for {
		if sess.tmpPkt != nil {
			pkt, sess.tmpPkt = sess.tmpPkt, nil
		} else {
			select {
			case pkt = <-sess.receive:
			case <-sess.closed:
				return nil
			}
		}

		switch {
		case pkt.SequenceNumber == sess.seq:
			sess.seq++
			return pkt
		case pkt.SequenceNumber-sess.seq <= sess.bufCap:
			sess.setBuf(pkt.SequenceNumber-sess.seq-1, pkt)
		default:
			bufLen := sess.bufLen
			for i := uint16(0); i < bufLen; i++ {
				read := sess.readBuf()
				if read != nil {
					sess.seq = read.SequenceNumber + 1
					sess.tmpPkt = pkt
					return read
				}
			}
			sess.seq = pkt.SequenceNumber + 1
			sess.emptyBuf()
			return pkt
		}
	}
}

func (sess *Session) readBuf() (pkt *Packet) {
	if sess.bufLen == 0 {
		return nil
	}
	pkt, sess.buf[sess.bufOffset] = sess.buf[sess.bufOffset], nil
	sess.bufOffset++
	sess.bufLen--
	if sess.bufOffset == sess.bufCap {
		sess.bufOffset = 0
	}
	return pkt
}

func (sess *Session) setBuf(offset uint16, pkt *Packet) {
	offset = (sess.bufOffset + offset) % sess.bufCap
	if offset+1 > sess.bufLen {
		sess.bufLen = offset + 1
	}
	sess.buf[offset] = pkt
}

func (sess *Session) emptyBuf() {
	sess.bufOffset = 0
	sess.bufLen = 0
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
