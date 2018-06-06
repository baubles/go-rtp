package rtp

import (
	"bytes"
)

type PSUnpacker struct {
	In  chan interface{}
	Out chan interface{}
}

func (unpacker *PSUnpacker) unpack() {
	buf := bytes.NewBuffer(make([]byte, 1024*1024))
	for {
		pkt := (<-unpacker.In).(*RtpPacket)
		buf.Write(pkt.Payload)
		if pkt.Marker {
			h264Buf, err := unpacker.getH264FromPS(buf.Bytes())
			if err == nil {
				// TODO
			}
			buf.Reset()
		}
	}
}

func (unpacker *PSUnpacker) getH264FromPS(buf []byte) (h264buf []byte, err error) {
	leftLength := 0

}

func (unpacker *PSUnpacker) checkPSHeader() {

}

type PSPackStart struct {
	StartCode [4]byte
	StreamId  byte
}

type PSPackHeader struct {
	PackStart PSPackStart
	Buf [9]
}
