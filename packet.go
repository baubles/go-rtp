package rtp

import (
	"bytes"
	"encoding/binary"
	"sync"
)

type Packet struct {
	Version        uint8
	Padding        bool
	Extension      bool
	Marker         bool
	PayloadType    uint8
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
	CSRCList       []uint32
	Payload        []byte

	pool *sync.Pool
	buf  []byte
}

func newPacket(pool *sync.Pool) *Packet {
	return &Packet{
		pool: pool,
		buf:  make([]byte, maxUDPPacketSize),
	}
}

func (packet *Packet) release() {
	if packet.pool != nil {
		packet.pool.Put(packet)
	}
}

func (packet *Packet) unmarshal(b []byte) (err error) {
	reader := bytes.NewReader(b)

	var first32 uint32
	err = binary.Read(reader, binary.BigEndian, &first32)
	if err != nil {
		return err
	}

	// Unmarshal first 32 bits
	packet.Version = uint8(first32 >> 30)
	packet.Padding = (first32 >> 29 & 1) > 0
	packet.Extension = (first32 >> 28 & 1) > 0
	CSRCCount := first32 >> 24 & 15
	packet.Marker = (first32 >> 23 & 1) > 0
	packet.PayloadType = uint8(first32 >> 16 & 127)
	packet.SequenceNumber = uint16(first32 & 65535)

	// Unmarshal timestamp
	err = binary.Read(reader, binary.BigEndian, &packet.Timestamp)
	if err != nil {
		return err
	}

	// Unmrashal SSRC
	err = binary.Read(reader, binary.BigEndian, &packet.SSRC)
	if err != nil {
		return err
	}

	// Unmarshal CSRC list
	packet.CSRCList = make([]uint32, CSRCCount)
	for i := 0; i < int(CSRCCount); i++ {
		err = binary.Read(reader, binary.BigEndian, &packet.CSRCList[i])
		if err != nil {
			return err
		}
	}

	// Unmarshal payload
	packet.Payload = packet.buf[:reader.Len()]
	reader.Read(packet.Payload)

	return nil
}
