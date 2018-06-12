package rtp

type Processor interface {
	Process(pkt *Packet)
	Attach(p Processor)
	Release()
}
