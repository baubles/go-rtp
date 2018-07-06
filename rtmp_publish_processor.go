package rtp

import (
	"fmt"
	"sync"

	rtmp "github.com/zhangpeihao/gortmp"
)

type rtmpPublishProcessor struct {
	next Processor
	mux  sync.Mutex

	handler *rtmpSinkHandler
	obConn  rtmp.OutboundConn
	stream  rtmp.OutboundStream
	url     string
	name    string
}

func NewRTMPPublishProcessor(url, name string) (p Processor, err error) {
	proc := &rtmpPublishProcessor{url: url, name: name}

	handler := newrtmpSinkHandler()
	handler.createStreamChan = make(chan rtmp.OutboundStream)
	handler.startPublishChan = make(chan rtmp.OutboundStream)
	proc.handler = handler

	proc.obConn, err = rtmp.Dial(url, handler, 100)
	if err != nil {
		return nil, err
	}

	err = proc.obConn.Connect()
	if err != nil {
		return nil, err
	}

	stream := <-handler.createStreamChan
	stream.Attach(handler)
	if err = stream.Publish(name, "live"); err != nil {
		return nil, err
	}

	proc.stream = <-handler.startPublishChan

	return proc, nil
}

func (proc *rtmpPublishProcessor) Process(packet interface{}) error {
	flvTag, ok := packet.(*FlvTag)
	if !ok {
		return fmt.Errorf("rtmpPublishProcessor process pkt is not *FlvTag")
	}
	select {
	case <-proc.handler.closed:
		logger.Printf("rtmp closed, rtmp %v, name %v\n", proc.url, proc.name)
		return fmt.Errorf("rtmp closed")
	default:
	}

	return proc.stream.PublishData(flvTag.TagType, flvTag.Data, flvTag.Timestamp)
}

func (proc *rtmpPublishProcessor) Attach(next Processor) {
	old := proc.next
	proc.next = next
	if old != nil {
		old.Release()
	}
}

func (proc *rtmpPublishProcessor) Release() {
	next := proc.next
	if next != nil {
		next.Release()
	}
}

func (proc *rtmpPublishProcessor) nextProcess(pkt interface{}) {
	proc.stream.Close()
	proc.obConn.Close()
	next := proc.next
	if next != nil {
		next.Process(pkt)
	}
}

type rtmpSinkHandler struct {
	status uint
	// obConn           rtmp.OutboundConn
	createStreamChan chan rtmp.OutboundStream
	startPublishChan chan rtmp.OutboundStream
	videoDataSize    int64
	audioDataSize    int64
	startPublish     bool
	closed           chan bool
}

func newrtmpSinkHandler() *rtmpSinkHandler {
	return &rtmpSinkHandler{
		closed: make(chan bool),
	}
}

func (handler *rtmpSinkHandler) OnStatus(conn rtmp.OutboundConn) {
	var err error
	handler.status, err = conn.Status()
	if err != nil {
		logger.Printf("rtmp status: %d, err: %v\n", handler.status, err)
	}
}

func (handler *rtmpSinkHandler) OnClosed(conn rtmp.Conn) {
	close(handler.closed)
	// logger.Printf("rtmp closed\n")
}

func (handler *rtmpSinkHandler) OnReceived(conn rtmp.Conn, message *rtmp.Message) {
}

func (handler *rtmpSinkHandler) OnReceivedRtmpCommand(conn rtmp.Conn, command *rtmp.Command) {
	// logger.Printf("rtmp receive command: %+v\n", command)
}

func (handler *rtmpSinkHandler) OnStreamCreated(conn rtmp.OutboundConn, stream rtmp.OutboundStream) {
	// logger.Printf("rtmp stream created: %d\n", stream.ID())
	handler.createStreamChan <- stream
}
func (handler *rtmpSinkHandler) OnPlayStart(stream rtmp.OutboundStream) {

}
func (handler *rtmpSinkHandler) OnPublishStart(stream rtmp.OutboundStream) {
	// Set chunk buffer size
	// logger.Printf("rtmp publish start\n")
	handler.startPublishChan <- stream
}
