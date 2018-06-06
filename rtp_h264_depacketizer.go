package rtp

import (
	"bytes"
	"fmt"
)

type RtpH264Depacketizer struct {
	InputChan  chan interface{}
	OutputChan chan interface{}
}

func NewRtpH264Depacketizer() *RtpH264Depacketizer {
	demuxer := &RtpH264Depacketizer{
		InputChan:  make(chan interface{}),
		OutputChan: make(chan interface{}),
	}

	go func() {
		var fragments []*RtpPacket

		for {
			packet := (<-demuxer.InputChan).(*RtpPacket)
			header := packet.Payload[0]
			// fmt.Println("h264 payload type:", header&31)
			switch header & 31 {
			// case 0, 31:
			// 	continue
			// case 24: // STAP-A
			// case 25: // STAP-B
			// case 26: // MTAP16
			// case 27: // MTAP24
			// case 28: // FU-A
			// case 29: // FU-B

			case 28:
				// FU-A
				fu_header := packet.Payload[1]
				if (fu_header>>7)&1 == 1 {
					// Start
					fragments = make([]*RtpPacket, 0, 2)
				}
				if len(fragments) != 0 && fragments[len(fragments)-1].SequenceNumber != packet.SequenceNumber-1 {
					fmt.Println("Packet loss?")
					fragments = nil
					continue
				}

				fragments = append(fragments, packet)

				if (fu_header>>6)&1 == 1 {
					// End
					// Payload := make([]byte, 0)
					// Payload = append(Payload, 0|(header&96)|(fu_header&31))
					// for _, fragment := range fragments {
					// 	Payload = append(Payload, fragment.Payload[2:]...)
					// }
					// packet.Payload = Payload
					// demuxer.OutputChan <- packet

					// fragments = nil

					buf := bytes.NewBuffer(make([]byte, 1024*1024))
					buf.WriteByte(0 | (header & 96) | (fu_header & 31))
					for _, fragment := range fragments {
						buf.Write(fragment.Payload[2:])
					}
					packet.Payload = buf.Bytes()
					demuxer.OutputChan <- packet
					fragments = nil
				}
			default:
				demuxer.OutputChan <- packet
			}
		}
	}()

	return demuxer
}

// type Coder interface {
// 	Input() chan interface{}
// 	Output() chan interface{}
// }

// type h264Decoder struct {
// 	in  chan interface{}
// 	out chan interface{}
// }

// func NewH264Decoder() Coder {
// 	// coder := &h264Decoder{
// 	// 	in:  make(chan interface{}),
// 	// 	out: make(chan interface{}),
// 	// }

// 	in := make(chan interface{})
// 	out := make(chan interface{})
// 	go func() {
// 		// for in
// 	}()
// }
