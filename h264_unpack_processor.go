package rtp

import (
	"bytes"
	"log"
	"sync"
)

type h264UnpackProcessor struct {
	next Processor
	mux  sync.Mutex

	fragments    []*Packet
	fragmentsLen int
}

func NewH264UnpackProcessor() Processor {
	return &h264UnpackProcessor{
		fragments: make([]*Packet, 0, 2),
	}
}

func (proc *h264UnpackProcessor) Attach(next Processor) {
	proc.mux.Lock()
	old := proc.next
	proc.next = next
	proc.mux.Unlock()
	if old != nil {
		old.Release()
	}
}

func (proc *h264UnpackProcessor) Release() {
	proc.mux.Lock()
	next := proc.next
	proc.mux.Unlock()
	next.Release()
}

func (proc *h264UnpackProcessor) Process(pkt *Packet) {
	header := pkt.Payload[0]
	switch header & 31 {
	case 28:
		// FU-A
		fu_header := pkt.Payload[1]
		if (fu_header>>7)&1 == 1 {
			// Start
			proc.fragmentsLen = 0
		}

		if proc.fragmentsLen != 0 && proc.fragments[proc.fragmentsLen-1].SequenceNumber != pkt.SequenceNumber-1 {
			log.Println("h264 unpack process: packet loss?")
			proc.fragmentsLen = 0
			return
		}

		proc.fragments[proc.fragmentsLen] = pkt
		proc.fragmentsLen++

		if (fu_header>>6)&1 == 1 {
			buf := bytes.NewBuffer(make([]byte, 1024*1024))
			buf.WriteByte(0 | (header & 96) | (fu_header & 31))

			for i := 0; i < proc.fragmentsLen; i++ {
				buf.Write(proc.fragments[i].Payload[2:])
			}
			pkt.Payload = buf.Bytes()
			proc.fragmentsLen = 0
			proc.nextProcess(pkt)
		}
	default:
		proc.fragmentsLen = 0
		proc.nextProcess(pkt)
	}

}

func (proc *h264UnpackProcessor) nextProcess(pkt *Packet) {
	proc.mux.Lock()
	next := proc.next
	proc.mux.Unlock()
	if next != nil {
		next.Process(pkt)
	}
}
