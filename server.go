package rtp

import (
	"fmt"
	"log"
	"net"
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

		pkt, err := UnmarshalPacket(buf[:n])
		if err != nil {
			log.Println("rtp packet unmarshal err:", err)
		}

		val, ok := srv.sessions.Load(pkt.SSRC)
		var sess *Session
		if ok {
			sess = val.(*Session)
		} else {
			sess = newSession(pkt.SSRC, raddr)
			srv.sessions.Store(pkt.SSRC, sess)
			select {
			case srv.accept <- sess:
				async(&srv.wg, func() {
					err := sess.process()
					if err != nil {
						sess.close()
						srv.sessions.Delete(sess.ssrc)
					}
				})
			default:
				srv.sessions.Delete(pkt.SSRC)
				log.Println("session can't be accepted, will be drop")
				continue
			}
		}

		select {
		case sess.receive <- pkt:
		default:
			log.Println("pkt can't be receive, will be drop")
		}
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
