package rtp

import (
	"bytes"
	"encoding/binary"
	"sync"

	amf "github.com/zhangpeihao/goamf"
)

type flvMuxerProcessor struct {
	SPS, PPS       []byte
	SPSSent        bool
	firstTimestamp uint32
	next           Processor
	mux            sync.Mutex
	lastTimestamp  uint32
	deltaTimestamp uint32
}

func NewFlvMuxerProcessor() Processor {
	proc := &flvMuxerProcessor{}

	return proc
}

func (proc *flvMuxerProcessor) Attach(next Processor) {
	proc.mux.Lock()
	old := proc.next
	proc.next = next
	proc.mux.Unlock()
	if old != nil {
		old.Release()
	}
}

func (proc *flvMuxerProcessor) Release() {
	proc.mux.Lock()
	next := proc.next
	proc.mux.Unlock()
	next.Release()
}

func (proc *flvMuxerProcessor) nextProcess(pkt interface{}) error {
	proc.mux.Lock()
	next := proc.next
	proc.mux.Unlock()
	if next != nil {
		return next.Process(pkt)
	}
	return nil
}

func (proc *flvMuxerProcessor) Process(pkt interface{}) error {
	packet, _ := pkt.(*Packet)
	if proc.firstTimestamp == 0 {
		proc.firstTimestamp = packet.Timestamp
	}

	deltaTimestamp := packet.Timestamp - proc.lastTimestamp
	if proc.deltaTimestamp == 0 || (deltaTimestamp < proc.deltaTimestamp && deltaTimestamp > 0) {
		proc.deltaTimestamp = deltaTimestamp
	}

	defer func() {
		proc.lastTimestamp = packet.Timestamp
	}()

	dts := uint32((packet.Timestamp-proc.firstTimestamp)/90) + 1000
	pts := dts + 500

	var videoDataPayload []byte

	nalt := packet.Payload[0] & 31

	switch nalt {
	case 7:
		proc.SPS = packet.Payload
	case 8:
		proc.PPS = packet.Payload
	}

	if nalt == 7 || nalt == 8 {
		if proc.SPS != nil && proc.PPS != nil && proc.SPSSent == false && proc.deltaTimestamp > 0 {
			metaData := &MetaData{
				FrameRate: 90000 / proc.deltaTimestamp,
				// HasVideo:     true,
				VideoCodecID: CODEC_AVC,
			}
			metaDataPayload := marshalMetaData(metaData)
			flvTag := &FlvTag{
				TagType:   TAG_SCRIPT,
				DataSize:  uint32(len(metaDataPayload)),
				Timestamp: 0,
				Data:      metaDataPayload,
			}

			if err := proc.nextProcess(flvTag); err != nil {
				return err
			}

			record := &AVCDecoderConfigurationRecord{
				ConfigurationVersion: 1,
				AVCProfileIndication: proc.SPS[1],
				ProfileCompatibility: proc.SPS[2],
				AVCLevelIndication:   proc.SPS[3],
				SPS:                  proc.SPS,
				PPS:                  proc.PPS,
			}

			videoData := &VideoData{
				FrameType:       FRAME_TYPE_KEY,
				CodecID:         CODEC_AVC,
				AVCPacketType:   AVC_SEQ_HEADER,
				CompositionTime: int32(pts - dts),
				Data:            marshalAVCDecoderConfigurationRecord(record),
			}
			videoDataPayload = marshalVideoData(videoData)
			proc.SPSSent = true
		} else {
			return nil
		}
	} else if proc.SPSSent {
		videoData := &VideoData{
			FrameType:       FRAME_TYPE_INTER,
			CodecID:         CODEC_AVC,
			AVCPacketType:   AVC_NALU,
			CompositionTime: int32(pts - dts),
			Data:            packet.Payload,
		}

		if nalt == 5 {
			videoData.FrameType = FRAME_TYPE_KEY
		}
		videoDataPayload = marshalVideoData(videoData)
	}

	if videoDataPayload == nil {
		return nil
	}

	flvTag := &FlvTag{
		TagType:   TAG_VIDEO,
		DataSize:  uint32(len(videoDataPayload)),
		Timestamp: dts,
		Data:      videoDataPayload,
	}

	return proc.nextProcess(flvTag)
}

// func (proc *flvMuxerProcessor) muxVideoPacket(packet *Packet, dts, pts uint32) []byte {
// 	var videoDataPayload []byte

// 	if packet.Payload[0]&31 == 7 {
// 		muxer.SPS = packet.Payload
// 	}
// 	if packet.Payload[0]&31 == 8 {
// 		muxer.PPS = packet.Payload
// 	}

// 	if packet.Payload[0]&31 == 7 || packet.Payload[0]&31 == 8 {
// 		if muxer.SPS != nil && muxer.PPS != nil && muxer.SPSSent == false {
// 			metaDataPayload := marshalMetaData(&MetaData{
// 				FrameRate:    25,
// 				HasVideo:     true,
// 				VideoCodecID: CODEC_AVC,
// 			})
// 			flvTag := &FlvTag{
// 				TagType:   TAG_SCRIPT,
// 				DataSize:  uint32(len(metaDataPayload)),
// 				Timestamp: 0,
// 				Data:      metaDataPayload,
// 			}
// 			muxer.nextProcess(flvTag)

// 			record := &AVCDecoderConfigurationRecord{CanSeekToEnd
// 				ConfigurationVersion: 1,
// 				AVCProfileIndication: muxer.SPS[1],
// 				ProfileCompatibility: muxer.SPS[2],
// 				AVCLevelIndication:   muxer.SPS[3],
// 				SPS:                  muxer.SPS,
// 				PPS:                  muxer.PPS,
// 			}

// 			videoData := &VideoData{
// 				FrameType:       FRAME_TYPE_KEY,
// 				CodecID:         CODEC_AVC,
// 				AVCPacketType:   AVC_SEQ_HEADER,
// 				CompositionTime: int32(pts - dts),
// 				Data:            marshalAVCDecoderConfigurationRecord(record),
// 			}
// 			videoDataPayload = marshalVideoData(videoData)
// 			// fmt.Println("SPS & PPS")
// 			muxer.SPSSent = true
// 		} else {
// 			return nil
// 		}
// 	}

// 	if proc. {
// 		videoData := &VideoData{
// 			FrameType:       FRAME_TYPE_INTER,
// 			CodecID:         CODEC_AVC,
// 			AVCPacketType:   AVC_NALU,
// 			CompositionTime: int32(pts - dts),
// 			Data:            packet.Payload,
// 		}
// 		// fmt.Println(packet.Payload[0] & 31)
// 		if packet.Payload[0]&31 == 5 {
// 			// fmt.Println("Key!")
// 			videoData.FrameType = FRAME_TYPE_KEY
// 		}
// 		videoDataPayload = marshalVideoData(videoData)
// 	}

// 	return videoDataPayload
// }

func (muxer *flvMuxerProcessor) muxAudioPacket(packet *Packet, dts, pts uint32) []byte {
	var audioDataPayload []byte

	audioData := &AudioData{
		SoundFormat:   SOUND_FORMAT_AAC,
		SoundRate:     SOUND_RATE_44,
		SoundSize:     SOUND_SIZE_8,
		SoundType:     SOUND_TYPE_STEREO,
		AACPacketType: AAC_RAW,
		Data:          packet.Payload,
	}
	audioDataPayload = marshalAudioData(audioData)

	return audioDataPayload
}

var FlvHeader []byte = []byte{0x46, 0x4c, 0x56, 0x01, 0x05, 0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00, 0x00}

type FlvTag struct {
	TagType   uint8
	DataSize  uint32
	Timestamp uint32
	Data      []byte
}

const (
	TAG_AUDIO  = 8
	TAG_VIDEO  = 9
	TAG_SCRIPT = 18
)

type VideoData struct {
	FrameType       uint8
	CodecID         uint8
	AVCPacketType   uint8
	CompositionTime int32
	Data            []byte
}

type AVCDecoderConfigurationRecord struct {
	ConfigurationVersion uint8
	AVCProfileIndication uint8
	ProfileCompatibility uint8
	AVCLevelIndication   uint8
	SPS                  []byte
	PPS                  []byte
}

type AudioData struct {
	SoundFormat   uint8
	SoundRate     uint8
	SoundSize     uint8
	SoundType     uint8
	AACPacketType uint8
	Data          []byte
}

type MetaData struct {
	// HasVideo      bool
	Width         uint32
	Height        uint32
	FrameRate     uint32
	VideoDataRate uint32
	VideoCodecID  uint8
	CanSeekToEnd  bool

	HasAudio        bool
	AudioSampleRate uint32
	AudioSampleSize uint32
	AudioChannels   uint32
	AudioSpecCfg    uint8
	AudioSpecCfgLen uint32
}

const (
	FRAME_TYPE_KEY        = 1
	FRAME_TYPE_INTER      = 2
	FRAME_TYPE_DISP_INTER = 3
	FRAME_TYPE_GEN_INTER  = 4
	FRAME_TYPE_INFO       = 5
)

const (
	CODEC_JPEG      = 1
	CODEC_H263      = 2
	CODEC_SCREEN    = 3
	CODEC_VP6       = 4
	CODEC_VP6_ALPHA = 5
	CODEC_SCREEN2   = 6
	CODEC_AVC       = 7
)

const (
	AVC_SEQ_HEADER = 0
	AVC_NALU       = 1
	AVC_SEQ_END    = 2
)

const (
	SOUND_FORMAT_AAC = 10
)

const (
	SOUND_RATE_44 = 3
)

const (
	SOUND_SIZE_8  = 0
	SOUND_SIZE_16 = 1
)

const (
	SOUND_TYPE_MONO   = 0
	SOUND_TYPE_STEREO = 1
)

const (
	AAC_HEADER = 0
	AAC_RAW    = 1
)

func marshalVideoData(videoData *VideoData) []byte {
	writer := bytes.NewBuffer(make([]byte, 0, 1024*1024))

	binary.Write(writer, binary.BigEndian, (videoData.FrameType<<4)|videoData.CodecID)
	binary.Write(writer, binary.BigEndian, int32(0)|(int32(videoData.AVCPacketType)<<24)|videoData.CompositionTime)
	if videoData.AVCPacketType == AVC_NALU {
		binary.Write(writer, binary.BigEndian, uint32(len(videoData.Data)))
	}
	writer.Write(videoData.Data)

	return writer.Bytes()
}

func marshalFlvTag(flvTag *FlvTag) []byte {
	writer := bytes.NewBuffer(make([]byte, 0, 1024*1024))

	binary.Write(writer, binary.BigEndian, uint32(0)|(uint32(flvTag.TagType)<<24)|flvTag.DataSize)
	binary.Write(writer, binary.BigEndian, flvTag.Timestamp<<8)
	writer.Write([]byte{0, 0, 0})
	writer.Write(flvTag.Data)

	return writer.Bytes()
}

func marshalAVCDecoderConfigurationRecord(record *AVCDecoderConfigurationRecord) []byte {
	writer := bytes.NewBuffer(make([]byte, 0, 1024*1024))

	binary.Write(writer, binary.BigEndian, record.ConfigurationVersion)
	binary.Write(writer, binary.BigEndian, record.AVCProfileIndication)
	binary.Write(writer, binary.BigEndian, record.ProfileCompatibility)
	binary.Write(writer, binary.BigEndian, record.AVCLevelIndication)
	binary.Write(writer, binary.BigEndian, uint8(0xff))
	binary.Write(writer, binary.BigEndian, uint8(0xe1))
	binary.Write(writer, binary.BigEndian, uint16(len(record.SPS)))
	writer.Write(record.SPS)
	binary.Write(writer, binary.BigEndian, uint8(0x01))
	binary.Write(writer, binary.BigEndian, uint16(len(record.PPS)))
	writer.Write(record.PPS)

	return writer.Bytes()
}

func marshalAudioData(audioData *AudioData) []byte {
	writer := bytes.NewBuffer(make([]byte, 0, 1024*1024))

	binary.Write(writer, binary.BigEndian, uint8(0)|(audioData.SoundFormat<<4)|(audioData.SoundRate<<2)|(audioData.SoundSize<<1)|(audioData.SoundType))
	binary.Write(writer, binary.BigEndian, audioData.AACPacketType)
	writer.Write(audioData.Data)

	return writer.Bytes()
}

func marshalMetaData(metaData *MetaData) []byte {
	writer := bytes.NewBuffer(make([]byte, 0, 1024))

	amf.WriteString(writer, "@setDataFrame")
	amf.WriteString(writer, "onMetaData")

	obj := amf.Object{
		"copyright": "baubles",
		// "hasVideo":     metaData.HasVideo,
		"hasAudio":     metaData.HasAudio,
		"canSeekToEnd": metaData.CanSeekToEnd,
		"framerate":    metaData.FrameRate,
		"videocodecid": metaData.VideoCodecID,
	}
	if metaData.Width > 0 {
		obj["width"] = metaData.Width
	}
	if metaData.Height > 0 {
		obj["height"] = metaData.Height
	}

	amf.WriteObject(writer, obj)

	return writer.Bytes()
}
