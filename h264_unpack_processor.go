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
		fragments: make([]*Packet, 100),
	}
}

func (proc *h264UnpackProcessor) Attach(next Processor) {
	old := proc.next
	proc.next = next
	if old != nil {
		old.Release()
	}
}

func (proc *h264UnpackProcessor) Release() {
	next := proc.next
	if next != nil {
		next.Release()
	}
}

func (proc *h264UnpackProcessor) Process(packet interface{}) error {
	pkt, _ := packet.(*Packet)
	header := pkt.Payload[0]
	switch header & 31 {
	// FU-A
	case 28:
		fuheader := pkt.Payload[1]
		if (fuheader>>7)&1 == 1 {
			// Start
			proc.fragmentsLen = 0
		}

		if proc.fragmentsLen != 0 && proc.fragments[proc.fragmentsLen-1].SequenceNumber != pkt.SequenceNumber-1 {
			log.Println("h264 unpack process: packet loss?")
			proc.fragmentsLen = 0
			return nil
		}

		proc.fragments[proc.fragmentsLen] = pkt
		proc.fragmentsLen++

		if (fuheader>>6)&1 == 1 {
			buf := bytes.NewBuffer(make([]byte, 0, 1024*1024))
			buf.WriteByte(0 | (header & 96) | (fuheader & 31))

			for i := 0; i < proc.fragmentsLen; i++ {
				buf.Write(proc.fragments[i].Payload[2:])
			}
			pkt.Payload = buf.Bytes()
			proc.fragmentsLen = 0
			return proc.nextProcess(pkt)
		}
	default:
		proc.fragmentsLen = 0
		return proc.nextProcess(pkt)
	}
	return nil
}

func (proc *h264UnpackProcessor) nextProcess(pkt interface{}) error {
	next := proc.next
	if next != nil {
		return next.Process(pkt)
	}
	return nil
}
