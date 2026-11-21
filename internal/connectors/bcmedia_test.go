package connectors

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func bcFrame(magic string, header, payload []byte) []byte {
	b := append([]byte(magic), header...)
	b = append(b, payload...)
	return append(b, make([]byte, (8-len(payload)%8)%8)...)
}

func bcAudio(payload []byte) []byte {
	h := binary.LittleEndian.AppendUint16(nil, uint16(len(payload)))
	h = binary.LittleEndian.AppendUint16(h, uint16(len(payload)))
	return bcFrame("05wb", h, payload)
}

func bcVideoFrame(magic string, payload []byte, extra int) []byte {
	h := append([]byte("H264"), binary.LittleEndian.AppendUint32(nil, uint32(len(payload)))...)
	h = binary.LittleEndian.AppendUint32(h, uint32(extra))
	h = append(h, make([]byte, 8+extra)...)
	return bcFrame(magic, h, payload)
}

// The doorbell's microphone is read out of reolink-cli's preview: its AAC
// frames are kept, its video, header and ADPCM are left, and a stream that
// starts mid-frame finds its place (#667).
func TestBcmediaAudio(t *testing.T) {
	adts1 := []byte{0xff, 0xf9, 0x60, 0x40, 1, 2, 3, 4, 5}
	adts2 := []byte{0xff, 0xf9, 0x60, 0x40, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	info := append([]byte("1001"), binary.LittleEndian.AppendUint32(nil, 32)...)
	info = append(info, make([]byte, 24)...)
	var s bytes.Buffer
	s.Write([]byte{0x12, 0x34, 0x56}) // the end of a frame before the capture began
	s.Write(info)
	s.Write(bcVideoFrame("00dc", bytes.Repeat([]byte{0, 0, 0, 1, 0x65}, 7), 4))
	s.Write(bcAudio(adts1))
	s.Write(bcVideoFrame("01dc", []byte("05wb inside a picture"), 0))
	s.Write(bcFrame("01wb", []byte{4, 0, 4, 0}, []byte{1, 0, 2, 0}))
	s.Write(bcAudio(adts2))
	s.Write(bcAudio([]byte{1, 2, 3})) // not ADTS
	s.Write([]byte("05w"))            // cut off as it ended

	var got [][]byte
	if err := bcmediaAudio(&s, func(b []byte) error { got = append(got, append([]byte(nil), b...)); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !bytes.Equal(got[0], adts1) || !bytes.Equal(got[1], adts2) {
		t.Fatalf("audio %x", got)
	}
}
