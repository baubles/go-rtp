package rtp

import (
	"bytes"
	"fmt"
	"log"
	"sync"
)

var psHeaderLen = 14
var psStartCodeLen = 4
var pseLen = 9
var pthLen = 6
var psmLen = 6

var PackInvalidError = fmt.Errorf("pack is invalid")

type psUnpackProcessor struct {
	firstMainFrame bool
	buf            []byte
	next           Processor
	mux            sync.Mutex
}

func NewPSUnpackProcessor() Processor {
	return &psUnpackProcessor{
		firstMainFrame: false,
		buf:            make([]byte, 1024*1024),
	}
}

func (proc *psUnpackProcessor) Attach(next Processor) {
	proc.mux.Lock()
	old := proc.next
	proc.next = next
	proc.mux.Unlock()
	if old != nil {
		old.Release()
	}
}

func (proc *psUnpackProcessor) Release() {
	proc.mux.Lock()
	next := proc.next
	proc.mux.Unlock()
	next.Release()
}

func (proc *psUnpackProcessor) Process(packet interface{}) error {
	pkt, ok := packet.(*Packet)
	if !ok {
		return fmt.Errorf("psUnpackProcessor process pkt is not *Packet")
	}
	buf := bytes.NewBuffer(proc.buf)
	buf.Write(pkt.Payload)
	if pkt.Marker {
		h264, err := proc.h264(buf.Bytes())
		if err != nil {
			log.Println("process unpack ps packet err:", err)
		}
		if len(h264) > 0 {
			pkt.Payload = h264
			return proc.nextProcess(pkt)
		}
	}
	return nil
}

func (proc *psUnpackProcessor) nextProcess(pkt interface{}) error {
	proc.mux.Lock()
	next := proc.next
	proc.mux.Unlock()
	if next != nil {
		return next.Process(pkt)
	}
	return nil
}

func (proc *psUnpackProcessor) h264(buf []byte) (h264buf []byte, err error) {
	if len(buf) < psHeaderLen {
		return nil, PackInvalidError
	}

	header := buf[:psHeaderLen]

	stuffingLen := int(header[13] & '\x07')
	offset := stuffingLen + psHeaderLen
	if offset >= len(buf) {
		return nil, PackInvalidError
	}
	next := buf[offset:]
	h264 := bytes.NewBuffer(make([]byte, 1024*1024))

	for len(next) >= psStartCodeLen {
		if proc.firstMainFrame && next[0] == '\x00' && next[1] == '\x00' && next[2] == '\x01' && next[3] == '\xE0' {
			// pes
			if pseLen >= len(next) {
				err = PackInvalidError
				break
			}
			pse := next[:pseLen]
			stuffingLen := int(pse[8])
			l := uint(pse[4])<<8 + uint(pse[5])
			size := int(l) - 2 - 1 - stuffingLen
			offset := pseLen + stuffingLen

			if size > 0 {
				if len(next) <= offset+size {
					h264.Write(next[offset:])
					break
				} else {
					h264.Write(next[offset : offset+size])
					next = next[offset+size:]
				}
			} else {
				offset := pseLen - 2 - 1 + int(l)
				if len(next) <= offset {
					break
				} else {
					next = next[offset:]
				}
			}
		} else if next[0] == '\x00' && next[1] == '\x00' && next[2] == '\x01' && next[3] == '\xBB' {
			if len(next) <= pthLen {
				err = PackInvalidError
				break
			}
			pth := next[:pthLen]
			l := uint(pth[4])<<8 + uint(pth[5])
			offset := pthLen + int(l)
			if len(next) <= offset {
				err = PackInvalidError
				break
			}
			next = next[offset:]
		} else if next[0] == '\x00' && next[1] == '\x00' && next[2] == '\x01' && next[3] == '\xBC' {
			if len(next) <= psmLen {
				err = PackInvalidError
				break
			}
			psm := next[:psmLen]
			l := uint(psm[4])<<8 + uint(psm[5])
			offset := int(l) + psmLen
			proc.firstMainFrame = true
			if len(next) <= offset {
				err = PackInvalidError
				break
			}
			next = next[offset:]
		} else {
			fmt.Printf("%x %x %x %x\n", next[0], next[1], next[2], next[3])
			err = PackInvalidError
			break
		}
	}
	return h264.Bytes(), err
}
