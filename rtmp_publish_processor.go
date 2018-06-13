package rtp

import (
	"log"
	"sync"

	rtmp "github.com/zhangpeihao/gortmp"
)

type rtmpPublishProcessor struct {
	next Processor
	mux  sync.Mutex

	handler *rtmpSinkHandler
	obConn  rtmp.OutboundConn
	stream  rtmp.OutboundStream
}

func NewRTMPPublishProcessor(url string) (p Processor, err error) {
	proc := &rtmpPublishProcessor{}

	handler := &rtmpSinkHandler{}
	handler.createStreamChan = make(chan rtmp.OutboundStream)

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
	if err = stream.Publish(url, ""); err != nil {
		return nil, err
	}

	return proc, nil
}

func (proc *rtmpPublishProcessor) Process(packet interface{}) error {
	flvTag, ok := packet.(*FlvTag)
	if !ok {
		log.Println("rtmpPublishProcessor process pkt is not *FlvTag")
	}
	if flvTag.TagType == TAG_VIDEO {
		return proc.stream.PublishVideoData(flvTag.Data, flvTag.Timestamp)
	} else {
		return proc.stream.PublishAudioData(flvTag.Data, flvTag.Timestamp)
	}
}

func (proc *rtmpPublishProcessor) Attach(next Processor) {
	proc.mux.Lock()
	old := proc.next
	proc.next = next
	proc.mux.Unlock()
	if old != nil {
		old.Release()
	}
}

func (proc *rtmpPublishProcessor) Release() {
	proc.mux.Lock()
	next := proc.next
	proc.mux.Unlock()
	next.Release()
}

func (proc *rtmpPublishProcessor) nextProcess(pkt interface{}) {
	proc.stream.Close()
	proc.obConn.Close()
	proc.mux.Lock()
	next := proc.next
	proc.mux.Unlock()
	if next != nil {
		next.Process(pkt)
	}
}

type rtmpSinkHandler struct {
	status uint
	// obConn           rtmp.OutboundConn
	createStreamChan chan rtmp.OutboundStream
	videoDataSize    int64
	audioDataSize    int64
}

func (handler *rtmpSinkHandler) OnStatus(conn rtmp.OutboundConn) {
	var err error
	handler.status, err = conn.Status()
	log.Printf("rtmp status: %d, err: %v\n", handler.status, err)
}

func (handler *rtmpSinkHandler) OnClosed(conn rtmp.Conn) {
	log.Printf("rtmp closed\n")
}

func (handler *rtmpSinkHandler) OnReceived(conn rtmp.Conn, message *rtmp.Message) {
}

func (handler *rtmpSinkHandler) OnReceivedRtmpCommand(conn rtmp.Conn, command *rtmp.Command) {
	log.Printf("rtmp receive command: %+v\n", command)
}

func (handler *rtmpSinkHandler) OnStreamCreated(conn rtmp.OutboundConn, stream rtmp.OutboundStream) {
	log.Printf("rtmp stream created: %d\n", stream.ID())
	handler.createStreamChan <- stream
}
func (handler *rtmpSinkHandler) OnPlayStart(stream rtmp.OutboundStream) {

}
func (handler *rtmpSinkHandler) OnPublishStart(stream rtmp.OutboundStream) {
	// Set chunk buffer size
	log.Printf("rtmp publish start\n")
}
