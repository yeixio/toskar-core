package connectors

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Reolink's live preview comes in its own container, BcMedia, which
// reolink-cli writes as it is: an info header, then video frames and the
// microphone's audio frames, each padded to 8 bytes (#667). Toskar keeps the
// audio, AAC in ADTS frames, and leaves the video.

const (
	bcInfoV1   = 0x31303031 // "1001"
	bcInfoV2   = 0x32303031 // "1002"
	bcAAC      = 0x62773530 // "05wb"
	bcADPCM    = 0x62773130 // "01wb"
	bcMaxFrame = 4 << 20
)

// bcVideo reports one of the video frames' magics, "00dc" to "09dc" for
// key frames and "01dc" and on for the rest: "0Ndc".
func bcVideo(magic uint32) bool {
	return magic&0xffff00ff == 0x63640030 && (magic>>8)&0xff >= '0' && (magic>>8)&0xff <= '9'
}

// bcmediaAudio reads a BcMedia stream and calls aac with each audio frame's
// ADTS bytes, until the stream ends or aac returns an error. A stream that
// starts or loses its place mid-frame finds the next frame it knows.
func bcmediaAudio(r io.Reader, aac func([]byte) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	skip := func(n int) error {
		_, err := br.Discard(n)
		return err
	}
	pad := func(n int) int { return (8 - n%8) % 8 }
	lost := 0
	for {
		word, err := br.Peek(4)
		if err != nil {
			return eof(err)
		}
		magic := binary.LittleEndian.Uint32(word)
		known := magic == bcInfoV1 || magic == bcInfoV2 || bcVideo(magic) || magic == bcAAC || magic == bcADPCM
		if !known {
			// Lost: move on a byte at a time until a frame starts again.
			lost++
			if lost > bcMaxFrame {
				return errors.New("the camera's preview isn't in a form Toskar reads")
			}
			if _, err := br.Discard(1); err != nil {
				return eof(err)
			}
			continue
		}
		lost = 0
		_, _ = br.Discard(4)
		switch {
		case magic == bcInfoV1 || magic == bcInfoV2:
			var size [4]byte
			if _, err := io.ReadFull(br, size[:]); err != nil {
				return eof(err)
			}
			n := int(binary.LittleEndian.Uint32(size[:]))
			if n < 8 || n > 1024 {
				return fmt.Errorf("a preview header of %d bytes isn't one reolink-cli writes", n)
			}
			if err := skip(n - 8); err != nil {
				return eof(err)
			}
		case bcVideo(magic):
			// Type, payload size, extra header size, microseconds, unknown.
			var h [20]byte
			if _, err := io.ReadFull(br, h[:]); err != nil {
				return eof(err)
			}
			payload := int(binary.LittleEndian.Uint32(h[4:8]))
			extra := int(binary.LittleEndian.Uint32(h[8:12]))
			if payload > bcMaxFrame || extra > 1024 {
				return errors.New("a preview frame is larger than any camera sends")
			}
			if err := skip(extra + payload + pad(payload)); err != nil {
				return eof(err)
			}
		case magic == bcAAC || magic == bcADPCM:
			var h [4]byte
			if _, err := io.ReadFull(br, h[:]); err != nil {
				return eof(err)
			}
			n := int(binary.LittleEndian.Uint16(h[0:2]))
			buf := make([]byte, n+pad(n))
			if _, err := io.ReadFull(br, buf); err != nil {
				return eof(err)
			}
			if magic == bcAAC && n >= 7 && buf[0] == 0xff && buf[1]&0xf0 == 0xf0 {
				if err := aac(buf[:n]); err != nil {
					return err
				}
			}
		}
	}
}

func eof(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}
