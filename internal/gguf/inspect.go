package gguf

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Details is what a GGUF file says about itself, read from its header, for
// a model added from a file (#467).
type Details struct {
	// Name is general.name; Architecture general.architecture.
	Name         string
	Architecture string
	// Type is general.type, such as "model" or "mmproj".
	Type string
	// SizeLabel is general.size_label, such as "8B".
	SizeLabel string
	// Parameters counts the weights in its tensors.
	Parameters uint64
	// ContextLength is {arch}.context_length, the longest window it was
	// trained for.
	ContextLength uint64
	// Quantization is the file type's name, such as Q4_K_M.
	Quantization string
	// ChatTemplate is set when the file carries tokenizer.chat_template.
	ChatTemplate bool
	// Thinks is set for a model that thinks before it answers (#840), by
	// its chat template or its name (thinks).
	Thinks bool
	// License is general.license, when stated.
	License string
	// Tensors is how many tensors it has.
	Tensors uint64
}

// Projector reports a vision projector (llama.cpp's mmproj), which
// describes pictures to a model and can't chat by itself.
func (d Details) Projector() bool {
	return d.Type == "mmproj" || d.Architecture == "clip"
}

// thinkingMarks are what a chat template has when its model thinks before
// it answers: a thinking block (Qwen 3, QwQ, Seed-OSS, Magistral), the
// switch that turns it off, a past turn's thinking, or gpt-oss's channels.
var thinkingMarks = []string{"<think>", "enable_thinking", "reasoning_content", "<|channel|>", "<seed:think>", "[THINK]"}

// thinkingName is a model that thinks whose template doesn't say so, such as
// DeepSeek R1's distills: their templates are their base model's, and
// llama.cpp opens the thinking itself.
var thinkingName = regexp.MustCompile(`(?i)\bR1\b|\bQwQ\b|thinking|reasoner`)

// thinks reports a model that thinks before it answers, from its name and
// chat template.
func thinks(name, template string) bool {
	for _, mark := range thinkingMarks {
		if strings.Contains(template, mark) {
			return true
		}
	}
	return thinkingName.MatchString(name)
}

// Limits on a header, far above any real model, so a damaged or hostile
// file is refused before it costs much memory.
const (
	maxTensors  = 1 << 20
	maxMetadata = 1 << 20
	maxDims     = 8
)

// ErrTruncated is a file shorter than its header says.
var ErrTruncated = errors.New("the file is incomplete: it ends before its last tensor")

// Inspect reads and checks a GGUF file's header: its metadata, and that
// every tensor lies inside the file, so a truncated or damaged download is
// refused before a runtime loads it. It reads only the header.
func Inspect(path string) (Details, error) {
	f, err := os.Open(path)
	if err != nil {
		return Details{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Details{}, err
	}
	cr := &countingReader{r: f}
	d := decoder{r: bufio.NewReaderSize(cr, 1<<16)}
	out, err := inspect(&d, uint64(st.Size()), func() uint64 { return cr.n - uint64(d.r.Buffered()) })
	if err != nil {
		return Details{}, err
	}
	if out.Quantization == "" {
		out.Quantization = quantFromName(filepath.Base(path))
	}
	return out, nil
}

type countingReader struct {
	r interface{ Read([]byte) (int, error) }
	n uint64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += uint64(n)
	return n, err
}

func inspect(d *decoder, size uint64, offset func() uint64) (Details, error) {
	var out Details
	if magic := d.bytes(4); d.err != nil || string(magic) != "GGUF" {
		if d.err != nil && size >= 4 {
			return out, d.err
		}
		return out, errors.New("not a GGUF file")
	}
	if version := d.u32(); d.err == nil && (version < 2 || version > 3) {
		return out, fmt.Errorf("GGUF version %d is not supported", version)
	}
	tensors := d.u64()
	count := d.u64()
	if d.err != nil {
		return out, d.err
	}
	if tensors == 0 || tensors > maxTensors || count > maxMetadata {
		return out, fmt.Errorf("the header lists %d tensors and %d values, which isn't a model", tensors, count)
	}
	out.Tensors = tensors
	alignment := uint64(32)
	ints := map[string]uint64{}
	var template string
	for n := uint64(0); n < count && d.err == nil; n++ {
		key := d.str()
		typ := d.u32()
		switch {
		case typ == typeString && (key == "general.architecture" || key == "general.name" || key == "general.type" || key == "general.size_label" || key == "general.license"):
			v := d.str()
			switch key {
			case "general.architecture":
				out.Architecture = v
			case "general.name":
				out.Name = v
			case "general.type":
				out.Type = v
			case "general.size_label":
				out.SizeLabel = v
			case "general.license":
				out.License = v
			}
		case key == "tokenizer.chat_template" && typ == typeString:
			out.ChatTemplate = true
			template = d.str()
		case key == "general.alignment" || key == "general.file_type" || strings.HasSuffix(key, ".context_length"):
			if v, ok := d.number(typ); ok {
				ints[key] = v
			}
		default:
			d.skip(typ, 0)
		}
	}
	if d.err != nil {
		return out, headerErr(d.err)
	}
	if a := ints["general.alignment"]; a != 0 {
		if a&(a-1) != 0 || a > 1<<16 {
			return out, fmt.Errorf("alignment %d isn't a power of two", a)
		}
		alignment = a
	}
	out.ContextLength = ints[out.Architecture+".context_length"]
	out.Thinks = thinks(out.Name, template)
	if ft, ok := ints["general.file_type"]; ok {
		out.Quantization = fileTypes[ft]
	}

	// Every tensor's place and size, to check against the file's length.
	var last struct {
		offset, bytes uint64
		known         bool
	}
	for n := uint64(0); n < tensors && d.err == nil; n++ {
		_ = d.str()
		dims := d.u32()
		if d.err == nil && (dims == 0 || dims > maxDims) {
			return out, fmt.Errorf("a tensor has %d dimensions", dims)
		}
		elements := uint64(1)
		for i := uint32(0); i < dims && d.err == nil; i++ {
			hi, lo := bits.Mul64(elements, d.u64())
			if hi != 0 {
				return out, errors.New("a tensor is impossibly large")
			}
			elements = lo
		}
		typ := d.u32()
		off := d.u64()
		if d.err != nil {
			break
		}
		if off%alignment != 0 {
			return out, errors.New("a tensor isn't aligned")
		}
		out.Parameters += elements
		if off >= last.offset {
			last.offset = off
			last.bytes, last.known = tensorBytes(typ, elements)
		}
	}
	if d.err != nil {
		return out, headerErr(d.err)
	}
	start := offset()
	if rem := start % alignment; rem != 0 {
		start += alignment - rem
	}
	if start > size {
		return out, ErrTruncated
	}
	data := size - start
	end := last.offset
	if last.known {
		end += last.bytes
	}
	if last.offset >= data || end > data {
		return out, ErrTruncated
	}
	return out, nil
}

// headerErr names a header that ends early.
func headerErr(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrTruncated
	}
	return err
}

// tensorBytes is a tensor's size in the file, for the types whose block
// layout is known; the rest skip the size check.
func tensorBytes(typ uint32, elements uint64) (uint64, bool) {
	t, ok := ggmlTypes[typ]
	if !ok || elements%t.block != 0 {
		return 0, false
	}
	blocks := elements / t.block
	if blocks > math.MaxUint64/t.bytes {
		return 0, false
	}
	return blocks * t.bytes, true
}

// ggmlTypes are the tensor types' block sizes: elements per block, and
// bytes per block (ggml's type traits).
var ggmlTypes = map[uint32]struct{ block, bytes uint64 }{
	0: {1, 4}, 1: {1, 2}, 2: {32, 18}, 3: {32, 20}, 6: {32, 22}, 7: {32, 24}, 8: {32, 34}, 9: {32, 36},
	10: {256, 84}, 11: {256, 110}, 12: {256, 144}, 13: {256, 176}, 14: {256, 210}, 15: {256, 292},
	16: {256, 66}, 17: {256, 74}, 18: {256, 98}, 19: {256, 50}, 20: {32, 18}, 21: {256, 110}, 22: {256, 82},
	23: {256, 136}, 24: {1, 1}, 25: {1, 2}, 26: {1, 4}, 27: {1, 8}, 28: {1, 8}, 29: {256, 56}, 30: {1, 2},
	34: {256, 54}, 35: {256, 66}, 39: {32, 17},
}

// fileTypes names general.file_type (llama.cpp's llama_ftype).
var fileTypes = map[uint64]string{
	0: "F32", 1: "F16", 2: "Q4_0", 3: "Q4_1", 7: "Q8_0", 8: "Q5_0", 9: "Q5_1", 10: "Q2_K",
	11: "Q3_K_S", 12: "Q3_K_M", 13: "Q3_K_L", 14: "Q4_K_S", 15: "Q4_K_M", 16: "Q5_K_S", 17: "Q5_K_M",
	18: "Q6_K", 19: "IQ2_XXS", 20: "IQ2_XS", 21: "Q2_K_S", 22: "IQ3_XS", 23: "IQ3_XXS", 24: "IQ1_S",
	25: "IQ4_NL", 26: "IQ3_S", 27: "IQ3_M", 28: "IQ2_S", 29: "IQ2_M", 30: "IQ4_XS", 31: "IQ1_M",
	32: "BF16", 36: "TQ1_0", 37: "TQ2_0", 38: "MXFP4_MOE",
}

var quantName = regexp.MustCompile(`(?i)(?:^|[-_.])((?:I?Q\d(?:_[A-Z0-9]+)*)|BF16|F16|F32)(?:[-_.]|$)`)

// quantFromName reads a quantization from a file name such as
// Llama-3.2-3B-Instruct-Q4_K_M.gguf, when the header doesn't say.
func quantFromName(name string) string {
	if m := quantName.FindStringSubmatch(strings.TrimSuffix(name, filepath.Ext(name))); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}
