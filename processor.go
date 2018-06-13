package rtp

type Processor interface {
	Process(pkt interface{}) error
	Attach(p Processor)
	Release()
}
