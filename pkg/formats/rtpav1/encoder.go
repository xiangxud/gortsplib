package rtpav1

import (
	"crypto/rand"
	"time"

	"github.com/bluenviron/mediacommon/pkg/codecs/av1"
	"github.com/pion/rtp"

	"github.com/bluenviron/gortsplib/v3/pkg/rtptime"
)

const (
	rtpVersion = 2
)

func randUint32() uint32 {
	var b [4]byte
	rand.Read(b[:])
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// Encoder is a RTP/AV1 encoder.
// Specification: https://aomediacodec.github.io/av1-rtp-spec/
type Encoder struct {
	// payload type of packets.
	PayloadType uint8

	// SSRC of packets (optional).
	// It defaults to a random value.
	SSRC *uint32

	// initial sequence number of packets (optional).
	// It defaults to a random value.
	InitialSequenceNumber *uint16

	// initial timestamp of packets (optional).
	// It defaults to a random value.
	InitialTimestamp *uint32

	// maximum size of packet payloads (optional).
	// It defaults to 1460.
	PayloadMaxSize int

	sequenceNumber uint16
	timeEncoder    *rtptime.Encoder
}

// Init initializes the encoder.
func (e *Encoder) Init() {
	if e.SSRC == nil {
		v := randUint32()
		e.SSRC = &v
	}
	if e.InitialSequenceNumber == nil {
		v := uint16(randUint32())
		e.InitialSequenceNumber = &v
	}
	if e.InitialTimestamp == nil {
		v := randUint32()
		e.InitialTimestamp = &v
	}
	if e.PayloadMaxSize == 0 {
		e.PayloadMaxSize = 1460 // 1500 (UDP MTU) - 20 (IP header) - 8 (UDP header) - 12 (RTP header)
	}

	e.sequenceNumber = *e.InitialSequenceNumber
	e.timeEncoder = rtptime.NewEncoder(90000, *e.InitialTimestamp)
}

// Encode encodes OBUs into RTP packets.
func (e *Encoder) Encode(obus [][]byte, pts time.Duration) ([]*rtp.Packet, error) {
	isKeyFrame, err := av1.ContainsKeyFrame(obus)
	if err != nil {
		return nil, err
	}

	ts := e.timeEncoder.Encode(pts)
	var curPacket *rtp.Packet
	var packets []*rtp.Packet
	curPayloadLen := 0

	createNewPacket := func(z bool) {
		curPacket = &rtp.Packet{
			Header: rtp.Header{
				Version:        rtpVersion,
				PayloadType:    e.PayloadType,
				SequenceNumber: e.sequenceNumber,
				Timestamp:      ts,
				SSRC:           *e.SSRC,
			},
			Payload: []byte{0},
		}
		e.sequenceNumber++
		packets = append(packets, curPacket)
		curPayloadLen = 1

		if z {
			curPacket.Payload[0] |= 1 << 7
		}
	}

	finalizeCurPacket := func(y bool) {
		if y {
			curPacket.Payload[0] |= 1 << 6
		}
	}

	createNewPacket(false)

	for _, obu := range obus {
		for {
			avail := e.PayloadMaxSize - curPayloadLen
			obuLen := len(obu)
			needed := obuLen + 2

			if needed <= avail {
				le := av1.LEB128Marshal(uint(obuLen))
				curPacket.Payload = append(curPacket.Payload, le...)
				curPacket.Payload = append(curPacket.Payload, obu...)
				curPayloadLen += len(le) + obuLen
				break
			}

			if avail > 2 {
				fragmentLen := avail - 2
				le := av1.LEB128Marshal(uint(fragmentLen))
				curPacket.Payload = append(curPacket.Payload, le...)
				curPacket.Payload = append(curPacket.Payload, obu[:fragmentLen]...)
				obu = obu[fragmentLen:]
			}

			finalizeCurPacket(true)
			createNewPacket(true)
		}
	}

	finalizeCurPacket(false)

	if isKeyFrame {
		packets[0].Payload[0] |= 1 << 3
	}

	packets[len(packets)-1].Marker = true

	return packets, nil
}
