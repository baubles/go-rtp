package rtp

import (
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"
)

var maxUDPPacketSize = 1600
var ErrServerClosed = fmt.Errorf("server be closed")

type Server struct {
	Addr          string
	ActiveTimeout time.Duration

	sessions *sync.Map
	accept   chan *Session
	wg       sync.WaitGroup

	mux      sync.Mutex
	listener *net.UDPConn
	closed   chan bool
	state    int8

	pktPool *sync.Pool
}

const (
	serverStatusReady    = 0
	serverStatusRunning  = 1
	serverStatusStopping = 2
)

func (srv *Server) Serve() (err error) {
	laddr, err := net.ResolveUDPAddr("udp", srv.Addr)
	if err != nil {
		return err
	}

	listener, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return err
	}

	if srv.pktPool == nil {
		srv.pktPool = &sync.Pool{}
		srv.pktPool.New = func() interface{} {
			return newPacket(srv.pktPool)
		}
	}

	srv.mux.Lock()
	if srv.state != serverStatusReady {
		srv.mux.Unlock()
		return fmt.Errorf("server is running")
	} else {
		srv.state = serverStatusRunning
		srv.mux.Unlock()
	}

	srv.closed = make(chan bool)
	srv.accept = make(chan *Session)
	srv.listener = listener
	srv.sessions = &sync.Map{}
	async(&srv.wg, srv.loopHandleRead)
	async(&srv.wg, srv.loopHandleUnactive)

	return nil
}

func (srv *Server) loopHandleUnactive() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		srv.sessions.Range(func(key interface{}, val interface{}) bool {
			sess := val.(*Session)
			if srv.ActiveTimeout > 0 && int64(srv.ActiveTimeout) < time.Now().UnixNano()-sess.lastActiveTime {
				sess.close()
			}
			return true
		})

		select {
		case <-ticker.C:
		case <-srv.closed:
			return
		}
	}
}

func (srv *Server) loopHandleRead() {
	for {
		buf := make([]byte, maxUDPPacketSize)
		n, raddr, err := srv.listener.ReadFrom(buf)
		if err != nil {
			break
		}

		pkt := srv.pktPool.Get().(*Packet)
		err = pkt.unmarshal(buf[:n])
		if err != nil {
			logger.Println("rtp packet unmarshal err:", err)
			pkt.release()
			continue
		}

		if pkt.Version != 2 {
			logger.Printf("rtp packet version support 2, receive %d\n", pkt.Version)
			pkt.release()
			continue
		}

		val, ok := srv.sessions.Load(pkt.SSRC)
		var sess *Session
		if ok {
			sess = val.(*Session)
			sess.lastActiveTime = time.Now().UnixNano()
		} else {
			sess = newSession(pkt.SSRC, raddr, srv)
			srv.sessions.Store(pkt.SSRC, sess)
			select {
			case srv.accept <- sess:
				async(&srv.wg, func() {
					err := sess.process()
					sess.close()
					srv.sessions.Delete(sess.ssrc)
					if err != nil {
						logger.Printf("process session ssrc=%v, err=%v\n", sess.ssrc, err)
					}
				})
			default:
				srv.sessions.Delete(pkt.SSRC)
				logger.Println("session can't be accepted, will be drop")
				goto NEXT
			}
		}
		select {
		case sess.receive <- pkt:
		default:
			logger.Printf("pkt can't be receive, will be drop. ssrc=%v seq=%v\n", pkt.SSRC, pkt.SequenceNumber)
		}
	NEXT:
		runtime.Gosched()
	}

	srv.sessions.Range(func(key interface{}, val interface{}) bool {
		sess := val.(*Session)
		sess.close()
		srv.sessions.Delete(key)
		return true
	})
}

func (srv *Server) Close() (err error) {
	srv.mux.Lock()
	if srv.state != serverStatusRunning {
		srv.mux.Unlock()
		return fmt.Errorf("server is not running")
	} else {
		srv.state = serverStatusStopping
		srv.mux.Unlock()
	}
	err = srv.listener.Close()
	close(srv.closed)
	srv.wg.Wait()
	srv.mux.Lock()
	srv.state = serverStatusReady
	srv.mux.Unlock()
	return err
}

func (srv *Server) Accept() (sess *Session, err error) {
	for {
		select {
		case sess = <-srv.accept:
			return sess, nil
		case <-srv.closed:
			return nil, ErrServerClosed
		}
	}
}

func async(wg *sync.WaitGroup, fn func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		fn()
	}()
}
