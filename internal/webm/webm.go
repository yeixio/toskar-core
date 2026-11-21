// Package webm reads and joins the WebM clips video.generate makes (#155):
// the last frame of a clip, to continue it from, and two clips as one.
// It knows the little of WebM those need: one VP8 video track, in
// SimpleBlocks, without lacing. ffmpeg isn't bundled, so this is ours.
package webm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"

	"golang.org/x/image/vp8"
)

// Element IDs, with their marker bits, as the Matroska spec lists them.
const (
	idEBML          = 0x1A45DFA3
	idSegment       = 0x18538067
	idInfo          = 0x1549A966
	idTimecodeScale = 0x2AD7B1
	idDuration      = 0x4489
	idTracks        = 0x1654AE6B
	idTrackEntry    = 0xAE
	idTrackNumber   = 0xD7
	idCodecID       = 0x86
	idVideo         = 0xE0
	idPixelWidth    = 0xB0
	idPixelHeight   = 0xBA
	idCluster       = 0x1F43B675
	idTimecode      = 0xE7
	idSimpleBlock   = 0xA3
	idBlockGroup    = 0xA0
	idBlock         = 0xA1
)

// Frame is one video frame: when it shows, in the clip's time units, and
// its VP8 data.
type Frame struct {
	Time     int64
	Keyframe bool
	Data     []byte
}

// Clip is a parsed WebM.
type Clip struct {
	// Header is the EBML header, as it was.
	Header []byte
	// Info is the segment's Info element without its Duration, as it was.
	info [][]byte
	// Tracks is the Tracks element, as it was.
	tracks []byte
	// Scale is nanoseconds per time unit; WebM's default is 1 ms.
	Scale  uint64
	Codec  string
	Width  int
	Height int
	Track  uint64
	Frames []Frame
}

// ErrNotKeyframe says a clip's last frame depends on the ones before it,
// which this package can't decode.
var ErrNotKeyframe = errors.New("the clip's last frame can't be read on its own")

type element struct {
	id   uint32
	body []byte
}

// readVint reads a variable-length integer; keep leaves the marker bit in,
// as element IDs do.
func readVint(b []byte, keep bool) (v uint64, n int, err error) {
	if len(b) == 0 {
		return 0, 0, errors.New("webm: truncated")
	}
	first := b[0]
	n = 1
	for mask := byte(0x80); n <= 8 && first&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 || len(b) < n {
		return 0, 0, errors.New("webm: bad number")
	}
	v = uint64(first)
	if !keep {
		v &= uint64(0xFF >> n)
	}
	for _, c := range b[1:n] {
		v = v<<8 | uint64(c)
	}
	return v, n, nil
}

// next reads one element at the start of b, and how many bytes it took.
// An element of unknown size takes the rest of b.
func next(b []byte) (element, int, error) {
	id, n, err := readVint(b, true)
	if err != nil {
		return element{}, 0, err
	}
	size, m, err := readVint(b[n:], false)
	if err != nil {
		return element{}, 0, err
	}
	start := n + m
	if size == uint64(1)<<(7*m)-1 {
		return element{id: uint32(id), body: b[start:]}, len(b), nil
	}
	if size > uint64(len(b)-start) {
		return element{}, 0, errors.New("webm: an element runs past the end")
	}
	end := start + int(size)
	return element{id: uint32(id), body: b[start:end]}, end, nil
}

// children splits b into elements. A cluster of unknown size ends at the
// next cluster, so it's split there.
func children(b []byte) ([]element, error) {
	var out []element
	for len(b) > 0 {
		e, n, err := next(b)
		if err != nil {
			return nil, err
		}
		if n == len(b) && e.id == idCluster {
			// Unknown size: find where the next cluster starts.
			if i := bytes.Index(e.body, []byte{0x1F, 0x43, 0xB6, 0x75}); i > 0 {
				n = offset(b, e.body) + i
				e.body = e.body[:i]
			}
		}
		out = append(out, e)
		b = b[n:]
	}
	return out, nil
}

// offset is where sub starts in b; sub must be a slice of b.
func offset(b, sub []byte) int { return cap(b) - cap(sub) }

func uintOf(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// Parse reads a clip.
func Parse(data []byte) (*Clip, error) {
	top, err := children(data)
	if err != nil {
		return nil, err
	}
	if len(top) < 2 || top[0].id != idEBML {
		return nil, errors.New("not a WebM file")
	}
	c := &Clip{Header: data[:offset(data, top[0].body)+len(top[0].body)], Scale: 1_000_000}
	var segment []byte
	for _, e := range top {
		if e.id == idSegment {
			segment = e.body
		}
	}
	if segment == nil {
		return nil, errors.New("webm: no segment")
	}
	parts, err := children(segment)
	if err != nil {
		return nil, err
	}
	for _, e := range parts {
		switch e.id {
		case idInfo:
			info, err := children(e.body)
			if err != nil {
				return nil, err
			}
			for _, x := range info {
				switch x.id {
				case idDuration:
				case idTimecodeScale:
					c.Scale = uintOf(x.body)
					c.info = append(c.info, raw(x))
				default:
					c.info = append(c.info, raw(x))
				}
			}
		case idTracks:
			c.tracks = raw(e)
			if err := c.readTracks(e.body); err != nil {
				return nil, err
			}
		case idCluster:
			if err := c.readCluster(e.body); err != nil {
				return nil, err
			}
		}
	}
	if c.Codec != "V_VP8" {
		return nil, fmt.Errorf("webm: the clip is %q, not VP8", c.Codec)
	}
	if len(c.Frames) == 0 {
		return nil, errors.New("webm: the clip has no frames")
	}
	return c, nil
}

func (c *Clip) readTracks(b []byte) error {
	entries, err := children(b)
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].id != idTrackEntry {
		return errors.New("webm: the clip must have one track")
	}
	fields, err := children(entries[0].body)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch f.id {
		case idTrackNumber:
			c.Track = uintOf(f.body)
		case idCodecID:
			c.Codec = string(f.body)
		case idVideo:
			video, err := children(f.body)
			if err != nil {
				return err
			}
			for _, v := range video {
				switch v.id {
				case idPixelWidth:
					c.Width = int(uintOf(v.body))
				case idPixelHeight:
					c.Height = int(uintOf(v.body))
				}
			}
		}
	}
	return nil
}

func (c *Clip) readCluster(b []byte) error {
	parts, err := children(b)
	if err != nil {
		return err
	}
	var base int64
	for _, p := range parts {
		switch p.id {
		case idTimecode:
			base = int64(uintOf(p.body))
		case idSimpleBlock:
			if err := c.readBlock(base, p.body); err != nil {
				return err
			}
		case idBlockGroup:
			inner, err := children(p.body)
			if err != nil {
				return err
			}
			for _, x := range inner {
				if x.id == idBlock {
					if err := c.readBlock(base, x.body); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (c *Clip) readBlock(base int64, b []byte) error {
	track, n, err := readVint(b, false)
	if err != nil {
		return err
	}
	if len(b) < n+3 {
		return errors.New("webm: a block is truncated")
	}
	rel := int16(binary.BigEndian.Uint16(b[n:]))
	flags := b[n+2]
	if flags&0x06 != 0 {
		return errors.New("webm: laced blocks aren't supported")
	}
	if track != c.Track {
		return nil
	}
	data := b[n+3:]
	// A VP8 frame is a keyframe when its first bit is 0, whatever the
	// container says.
	key := len(data) > 0 && data[0]&1 == 0
	c.Frames = append(c.Frames, Frame{Time: base + int64(rel), Keyframe: key, Data: data})
	return nil
}

func raw(e element) []byte {
	var buf bytes.Buffer
	writeElement(&buf, e.id, e.body)
	return buf.Bytes()
}

// LastFrame is the clip's last frame as a picture.
func (c *Clip) LastFrame() (image.Image, error) {
	f := c.Frames[len(c.Frames)-1]
	if !f.Keyframe {
		return nil, ErrNotKeyframe
	}
	d := vp8.NewDecoder()
	d.Init(bytes.NewReader(f.Data), len(f.Data))
	if _, err := d.DecodeFrameHeader(); err != nil {
		return nil, fmt.Errorf("webm: last frame: %w", err)
	}
	img, err := d.DecodeFrame()
	if err != nil {
		return nil, fmt.Errorf("webm: last frame: %w", err)
	}
	return img, nil
}

// step is the time between frames, from the clip's own spacing.
func (c *Clip) step() int64 {
	if len(c.Frames) < 2 {
		return int64(math.Round(1e9 / 16 / float64(c.Scale)))
	}
	return (c.Frames[len(c.Frames)-1].Time - c.Frames[0].Time) / int64(len(c.Frames)-1)
}

// Seconds is how long the clip plays.
func (c *Clip) Seconds() float64 {
	last := c.Frames[len(c.Frames)-1].Time + c.step()
	return float64(last-c.Frames[0].Time) * float64(c.Scale) / 1e9
}

// Join plays b after a, as one clip. When b starts from a's last frame,
// as a continued clip does, that frame is shown once.
func Join(a, b *Clip, continued bool) ([]byte, error) {
	if a.Codec != b.Codec || a.Width != b.Width || a.Height != b.Height {
		return nil, fmt.Errorf("the clips differ: %s %dx%d and %s %dx%d", a.Codec, a.Width, a.Height, b.Codec, b.Width, b.Height)
	}
	if a.Scale != b.Scale {
		return nil, errors.New("the clips keep time differently")
	}
	frames := append([]Frame(nil), a.Frames...)
	last := a.Frames[len(a.Frames)-1].Time
	rest, shift := b.Frames, last+a.step()-b.Frames[0].Time
	if continued && len(rest) > 1 {
		rest, shift = rest[1:], last-b.Frames[0].Time
	}
	for _, f := range rest {
		f.Time += shift
		frames = append(frames, f)
	}
	start := frames[0].Time
	end := frames[len(frames)-1].Time + b.step()

	var seg bytes.Buffer
	var info bytes.Buffer
	for _, x := range a.info {
		info.Write(x)
	}
	var dur [8]byte
	binary.BigEndian.PutUint64(dur[:], math.Float64bits(float64(end-start)))
	writeElement(&info, idDuration, dur[:])
	writeElement(&seg, idInfo, info.Bytes())
	seg.Write(a.tracks)
	for _, f := range frames {
		var cluster bytes.Buffer
		writeElement(&cluster, idTimecode, uintBytes(uint64(f.Time-start)))
		var block bytes.Buffer
		block.Write(vintBytes(a.Track))
		block.Write([]byte{0, 0})
		flags := byte(0)
		if f.Keyframe {
			flags = 0x80
		}
		block.WriteByte(flags)
		block.Write(f.Data)
		writeElement(&cluster, idSimpleBlock, block.Bytes())
		writeElement(&seg, idCluster, cluster.Bytes())
	}
	var out bytes.Buffer
	out.Write(a.Header)
	writeElement(&out, idSegment, seg.Bytes())
	return out.Bytes(), nil
}

func writeElement(buf *bytes.Buffer, id uint32, body []byte) {
	switch {
	case id > 0xFFFFFF:
		buf.Write([]byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)})
	case id > 0xFFFF:
		buf.Write([]byte{byte(id >> 16), byte(id >> 8), byte(id)})
	case id > 0xFF:
		buf.Write([]byte{byte(id >> 8), byte(id)})
	default:
		buf.WriteByte(byte(id))
	}
	// Sizes are written in eight bytes, which any size fits.
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(body)))
	size[0] = 0x01
	buf.Write(size[:])
	buf.Write(body)
}

func uintBytes(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	i := 0
	for i < 7 && b[i] == 0 {
		i++
	}
	return b[i:]
}

// vintBytes writes a small number, such as a track number, as a vint.
func vintBytes(v uint64) []byte {
	if v < 0x7F {
		return []byte{0x80 | byte(v)}
	}
	return []byte{0x40 | byte(v>>8), byte(v)}
}
