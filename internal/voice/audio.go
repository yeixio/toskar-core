package voice

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// SampleRate is the live voice protocol's audio: 16-bit little-endian PCM,
// mono, at 24 kHz, as OpenAI's Realtime API's pcm16 is.
const SampleRate = 24000

// DecodePCM16 reads little-endian 16-bit samples.
func DecodePCM16(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return out
}

// EncodePCM16 writes samples as little-endian 16-bit PCM.
func EncodePCM16(samples []int16) []byte {
	out := make([]byte, 2*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(s))
	}
	return out
}

// WAV is samples at rate as a mono 16-bit WAV file, as Whisper reads.
func WAV(samples []int16, rate int) []byte {
	data := EncodePCM16(samples)
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

// ParseWAV reads a 16-bit PCM WAV file, such as Piper's, mixing channels
// down to mono.
func ParseWAV(b []byte) (samples []int16, rate int, err error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a WAV file")
	}
	var channels, bits int
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4:]))
		body := pos + 8
		end := body + size
		if end > len(b) {
			end = len(b)
		}
		switch id {
		case "fmt ":
			if end-body < 16 {
				return nil, 0, errors.New("short WAV format chunk")
			}
			if format := binary.LittleEndian.Uint16(b[body:]); format != 1 {
				return nil, 0, fmt.Errorf("WAV format %d isn't PCM", format)
			}
			channels = int(binary.LittleEndian.Uint16(b[body+2:]))
			rate = int(binary.LittleEndian.Uint32(b[body+4:]))
			bits = int(binary.LittleEndian.Uint16(b[body+14:]))
		case "data":
			if bits != 16 || channels < 1 {
				return nil, 0, errors.New("WAV isn't 16-bit PCM")
			}
			all := DecodePCM16(b[body:end])
			if channels == 1 {
				return all, rate, nil
			}
			mono := make([]int16, len(all)/channels)
			for i := range mono {
				sum := 0
				for c := 0; c < channels; c++ {
					sum += int(all[i*channels+c])
				}
				mono[i] = int16(sum / channels)
			}
			return mono, rate, nil
		}
		pos = body + size + size%2
	}
	return nil, 0, errors.New("WAV has no data")
}

// Resample changes samples from one rate to another, linearly: enough for
// speech, such as Piper's 22.05 kHz to the protocol's 24 kHz.
func Resample(samples []int16, from, to int) []int16 {
	if from == to || from <= 0 || to <= 0 || len(samples) == 0 {
		return samples
	}
	n := int(int64(len(samples)) * int64(to) / int64(from))
	out := make([]int16, n)
	step := float64(from) / float64(to)
	for i := range out {
		x := float64(i) * step
		j := int(x)
		if j >= len(samples)-1 {
			out[i] = samples[len(samples)-1]
			continue
		}
		f := x - float64(j)
		out[i] = int16(math.Round(float64(samples[j])*(1-f) + float64(samples[j+1])*f))
	}
	return out
}

// level is a frame's loudness, its RMS from 0 to 1.
func level(frame []int16) float64 {
	if len(frame) == 0 {
		return 0
	}
	var sum float64
	for _, s := range frame {
		v := float64(s) / 32768
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(frame)))
}

// ms is how long n samples play.
func ms(n int) int { return n * 1000 / SampleRate }
