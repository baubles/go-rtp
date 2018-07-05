package rtp

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"sync"

	amf "github.com/zhangpeihao/goamf"
)

type flvMuxerProcessor struct {
	SPS, PPS                      []byte
	SPSSent                       bool
	firstTimestamp                uint32
	next                          Processor
	mux                           sync.Mutex
	lastTimestamp                 uint32
	deltaTimestamp                uint32
	videoData                     *bytes.Buffer
	audioData                     *bytes.Buffer
	avcDecoderConfigurationRecord *bytes.Buffer
	metaData                      *bytes.Buffer
}

func NewFlvMuxerProcessor() Processor {
	proc := &flvMuxerProcessor{
		videoData:                     new(bytes.Buffer),
		audioData:                     new(bytes.Buffer),
		avcDecoderConfigurationRecord: new(bytes.Buffer),
		metaData:                      new(bytes.Buffer),
	}

	return proc
}

func (proc *flvMuxerProcessor) Attach(next Processor) {
	old := proc.next
	proc.next = next
	if old != nil {
		old.Release()
	}
}

func (proc *flvMuxerProcessor) Release() {
	next := proc.next
	next.Release()
}

func (proc *flvMuxerProcessor) nextProcess(pkt interface{}) error {
	next := proc.next
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

	dts := uint32((packet.Timestamp - proc.firstTimestamp) / 90)
	pts := dts + 10

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
			sps := unmarshalH264SPS(proc.SPS)
			if sps == nil {
				return nil
			}

			metaData := &MetaData{
				FrameRate:    90000 / proc.deltaTimestamp,
				HasVideo:     true,
				Height:       (sps.PicHeightInMapUnitsMinus1 + 1) * 16,
				Width:        (sps.PicWidthInMbsMinus1 + 1) * 16,
				VideoCodecID: CODEC_AVC,
			}
			proc.metaData.Reset()
			metaData.WriteTo(proc.metaData)
			metaDataPayload := proc.metaData.Bytes()
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
			proc.avcDecoderConfigurationRecord.Reset()
			record.WriteTo(proc.avcDecoderConfigurationRecord)
			videoData := &VideoData{
				FrameType:       FRAME_TYPE_KEY,
				CodecID:         CODEC_AVC,
				AVCPacketType:   AVC_SEQ_HEADER,
				CompositionTime: int32(pts - dts),
				Data:            proc.avcDecoderConfigurationRecord.Bytes(),
			}
			proc.videoData.Reset()
			videoData.WriteTo(proc.videoData)
			videoDataPayload = proc.videoData.Bytes()
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
		proc.videoData.Reset()
		videoData.WriteTo(proc.videoData)
		videoDataPayload = proc.videoData.Bytes()
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

func (proc *flvMuxerProcessor) muxAudioPacket(packet *Packet, dts, pts uint32) []byte {
	var audioDataPayload []byte

	audioData := &AudioData{
		SoundFormat:   SOUND_FORMAT_AAC,
		SoundRate:     SOUND_RATE_44,
		SoundSize:     SOUND_SIZE_8,
		SoundType:     SOUND_TYPE_STEREO,
		AACPacketType: AAC_RAW,
		Data:          packet.Payload,
	}

	proc.audioData.Reset()
	audioData.WriteTo(proc.audioData)
	audioDataPayload = proc.audioData.Bytes()

	return audioDataPayload
}

var FlvHeader []byte = []byte{0x46, 0x4c, 0x56, 0x01, 0x05, 0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00, 0x00}

type FlvTag struct {
	TagType   uint8
	DataSize  uint32
	Timestamp uint32
	Data      []byte
}

func (flvTag *FlvTag) WriteTo(writer io.Writer) (err error) {
	if err = binary.Write(writer, binary.BigEndian, uint32(0)|(uint32(flvTag.TagType)<<24)|flvTag.DataSize); err != nil {
		return err
	}
	if err = binary.Write(writer, binary.BigEndian, flvTag.Timestamp<<8); err != nil {
		return err
	}
	if _, err = writer.Write([]byte{0, 0, 0}); err != nil {
		return err
	}
	writer.Write(flvTag.Data)
	return err
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

func (videoData *VideoData) WriteTo(writer io.Writer) (err error) {
	if err = binary.Write(writer, binary.BigEndian, (videoData.FrameType<<4)|videoData.CodecID); err != nil {
		return err
	}
	if err = binary.Write(writer, binary.BigEndian, int32(0)|(int32(videoData.AVCPacketType)<<24)|videoData.CompositionTime); err != nil {
		return err
	}
	if videoData.AVCPacketType == AVC_NALU {
		if err = binary.Write(writer, binary.BigEndian, uint32(len(videoData.Data))); err != nil {
			return err
		}
	}
	_, err = writer.Write(videoData.Data)
	return err
}

type AVCDecoderConfigurationRecord struct {
	ConfigurationVersion uint8
	AVCProfileIndication uint8
	ProfileCompatibility uint8
	AVCLevelIndication   uint8
	SPS                  []byte
	PPS                  []byte
}

func (record *AVCDecoderConfigurationRecord) WriteTo(writer io.Writer) (err error) {
	if err = binary.Write(writer, binary.BigEndian, record.ConfigurationVersion); err != nil {
		return err
	}
	if err = binary.Write(writer, binary.BigEndian, record.AVCProfileIndication); err != nil {
		return nil
	}
	if err = binary.Write(writer, binary.BigEndian, record.ProfileCompatibility); err != nil {
		return nil
	}
	if err = binary.Write(writer, binary.BigEndian, record.AVCLevelIndication); err != nil {
		return nil
	}
	if err = binary.Write(writer, binary.BigEndian, uint8(0xff)); err != nil {
		return nil
	}
	if err = binary.Write(writer, binary.BigEndian, uint8(0xe1)); err != nil {
		return nil
	}
	if err = binary.Write(writer, binary.BigEndian, uint16(len(record.SPS))); err != nil {
		return nil
	}
	if _, err = writer.Write(record.SPS); err != nil {
		return nil
	}
	if err = binary.Write(writer, binary.BigEndian, uint8(0x01)); err != nil {
		return nil
	}
	if err = binary.Write(writer, binary.BigEndian, uint16(len(record.PPS))); err != nil {
		return nil
	}
	_, err = writer.Write(record.PPS)
	return err
}

type AudioData struct {
	SoundFormat   uint8
	SoundRate     uint8
	SoundSize     uint8
	SoundType     uint8
	AACPacketType uint8
	Data          []byte
}

func (audioData *AudioData) WriteTo(writer io.Writer) (err error) {
	if err = binary.Write(writer, binary.BigEndian, uint8(0)|(audioData.SoundFormat<<4)|(audioData.SoundRate<<2)|(audioData.SoundSize<<1)|(audioData.SoundType)); err != nil {
		return err
	}
	if err = binary.Write(writer, binary.BigEndian, audioData.AACPacketType); err != nil {
		return err
	}
	_, err = writer.Write(audioData.Data)
	return err
}

type MetaData struct {
	HasVideo      bool
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

func (metaData *MetaData) WriteTo(writer amf.Writer) (err error) {
	if _, err = amf.WriteString(writer, "@setDataFrame"); err != nil {
		return err
	}
	if _, err = amf.WriteString(writer, "onMetaData"); err != nil {
		return err
	}

	obj := amf.Object{
		"copyright":    "baubles",
		"hasVideo":     metaData.HasVideo,
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

	_, err = amf.WriteObject(writer, obj)
	return err
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

func ue(data []byte, startBit *uint32) uint32 {
	zeroNum := uint32(0)
	for *startBit < uint32(len(data))*8 {
		if data[*startBit/8]&(0x80>>(*startBit%8)) != 0 {
			break
		}
		zeroNum++
		*startBit++
	}
	*startBit++

	val := uint32(0)

	for i := uint32(0); i < zeroNum; i++ {
		val <<= 1
		if data[*startBit/8]&(0x88>>(*startBit%8)) != 0 {
			val++
		}
		*startBit++
	}
	return (1 << zeroNum) - 1 + val
}

func se(data []byte, startBit *uint32) int32 {
	ueval := ue(data, startBit)
	val := math.Ceil(float64(ueval) / 2)
	if ueval%2 == 0 {
		val = -val
	}
	return int32(val)
}

func u(bitCount uint32, data []byte, startBit *uint32) uint32 {
	val := uint32(0)
	for i := uint32(0); i < bitCount; i++ {
		val <<= 1
		if data[*startBit/8]&(0x80>>(*startBit%8)) != 0 {
			val++
		}
		*startBit++
	}
	return val
}

type SPS struct {
	ForbiddenZeroBit                uint32
	NalRefIdc                       uint32
	NalUnitType                     uint32
	ProfileIdc                      uint32
	ConstraintSet0Flag              uint32
	ConstraintSet1Flag              uint32
	ConstraintSet2Flag              uint32
	ConstraintSet3Flag              uint32
	ReservedZero4Bits               uint32
	LevelIdc                        uint32
	SeqParameterSetID               uint32
	ChromaFormatIdc                 uint32
	ResidualColourTransformFlag     uint32
	BitDepthLumaMinus8              uint32
	BitDepthChromaMinus8            uint32
	QpprimeYZeroTransformBypassFlag uint32
	SeqScalingMatrixPresentFlag     uint32
	SeqScalingListPresentFlag       []uint32
	Log2MaxFrameNumMinus4           uint32
	PicOrderCntType                 uint32
	Log2MaxPicOrderCntLsbMinus4     uint32
	DeltaPicOrderAlwaysZeroFlag     uint32
	OffsetForNonRefPic              int32
	OffsetForTopToBottomField       int32
	NumRefFramesInPicOrderCntCycle  uint32
	OffsetForRefFrame               []int32
	NumRefFrames                    uint32
	GapsInFrameNumValueAllowedFlag  uint32
	PicWidthInMbsMinus1             uint32
	PicHeightInMapUnitsMinus1       uint32
}

func unmarshalH264SPS(data []byte) *SPS {
	start := uint32(0)
	startBit := &start
	sps := new(SPS)
	sps.ForbiddenZeroBit = u(1, data, startBit)
	sps.NalRefIdc = u(2, data, startBit)
	sps.NalUnitType = u(5, data, startBit)
	if sps.NalUnitType == 7 {
		sps.ProfileIdc = u(8, data, startBit)
		sps.ConstraintSet0Flag = u(1, data, startBit) //(buf[1] & 0x80)>>7;
		sps.ConstraintSet1Flag = u(1, data, startBit) //(buf[1] & 0x40)>>6;
		sps.ConstraintSet2Flag = u(1, data, startBit) //(buf[1] & 0x20)>>5;
		sps.ConstraintSet3Flag = u(1, data, startBit) //(buf[1] & 0x10)>>4;
		sps.ReservedZero4Bits = u(4, data, startBit)
		sps.LevelIdc = u(8, data, startBit)
		sps.SeqParameterSetID = ue(data, startBit)

		if sps.ProfileIdc == 100 || sps.ProfileIdc == 110 || sps.ProfileIdc == 122 || sps.ProfileIdc == 144 {
			sps.ChromaFormatIdc = ue(data, startBit)
			if sps.ChromaFormatIdc == 3 {
				sps.ResidualColourTransformFlag = u(1, data, startBit)
			}

			sps.BitDepthLumaMinus8 = ue(data, startBit)
			sps.BitDepthChromaMinus8 = ue(data, startBit)
			sps.QpprimeYZeroTransformBypassFlag = u(1, data, startBit)
			sps.SeqScalingMatrixPresentFlag = u(1, data, startBit)

			sps.SeqScalingListPresentFlag = make([]uint32, 8)
			if sps.SeqScalingMatrixPresentFlag != 0 {
				for i := 0; i < 8; i++ {
					sps.SeqScalingListPresentFlag[i] = u(1, data, startBit)
				}
			}
		}
		sps.Log2MaxFrameNumMinus4 = ue(data, startBit)
		sps.PicOrderCntType = ue(data, startBit)
		if sps.PicOrderCntType == 0 {
			sps.Log2MaxPicOrderCntLsbMinus4 = ue(data, startBit)
		} else if sps.PicOrderCntType == 1 {
			sps.DeltaPicOrderAlwaysZeroFlag = u(1, data, startBit)
			sps.OffsetForNonRefPic = se(data, startBit)
			sps.OffsetForTopToBottomField = se(data, startBit)
			sps.NumRefFramesInPicOrderCntCycle = ue(data, startBit)

			sps.OffsetForRefFrame = make([]int32, int(sps.NumRefFramesInPicOrderCntCycle))
			for i := 0; i < int(sps.NumRefFramesInPicOrderCntCycle); i++ {
				sps.OffsetForRefFrame[i] = se(data, startBit)
			}
		}
		sps.NumRefFrames = ue(data, startBit)
		sps.GapsInFrameNumValueAllowedFlag = u(1, data, startBit)
		sps.PicWidthInMbsMinus1 = ue(data, startBit)
		sps.PicHeightInMapUnitsMinus1 = ue(data, startBit)

		return sps
	}

	return nil
}
