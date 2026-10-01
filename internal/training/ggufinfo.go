package training

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// ggufTensor is one tensor's name and shape from a GGUF header.
type ggufTensor struct {
	Name string
	Dims []uint64
}

// readGGUFTensors reads the tensor names and shapes from a GGUF file's
// header, without reading the tensor data.
func readGGUFTensors(path string) ([]ggufTensor, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := &ggufReader{r: bufio.NewReader(f)}
	var magic [4]byte
	r.read(&magic)
	if r.err == nil && string(magic[:]) != "GGUF" {
		return nil, fmt.Errorf("%s is not a GGUF file", path)
	}
	version := r.u32()
	if r.err == nil && version < 2 {
		return nil, fmt.Errorf("GGUF version %d is not supported", version)
	}
	nTensors, nKV := r.u64(), r.u64()
	if r.err == nil && (nTensors > 1<<20 || nKV > 1<<20) {
		return nil, fmt.Errorf("%s has an implausible GGUF header", path)
	}
	for i := uint64(0); i < nKV && r.err == nil; i++ {
		r.str()
		r.skipValue(r.u32(), 0)
	}
	out := make([]ggufTensor, 0, nTensors)
	for i := uint64(0); i < nTensors && r.err == nil; i++ {
		t := ggufTensor{Name: r.str()}
		n := r.u32()
		if n > 8 {
			return nil, fmt.Errorf("tensor %s has %d dimensions", t.Name, n)
		}
		for j := uint32(0); j < n; j++ {
			t.Dims = append(t.Dims, r.u64())
		}
		r.u32() // type
		r.u64() // offset
		out = append(out, t)
	}
	if r.err != nil {
		return nil, fmt.Errorf("read GGUF header of %s: %w", path, r.err)
	}
	return out, nil
}

// mergedGrowth estimates how many bytes merging a LoRA adapter adds to its
// base model. llama-export-lora writes each tensor the adapter changes as
// F16 and copies the others unchanged, so the merged file is at most the
// base plus two bytes per weight of each changed tensor.
func mergedGrowth(adapter []ggufTensor) uint64 {
	dims := map[string][]uint64{}
	for _, t := range adapter {
		dims[t.Name] = t.Dims
	}
	var total uint64
	for name, a := range dims {
		stem, ok := strings.CutSuffix(name, ".lora_a")
		if !ok {
			continue
		}
		b := dims[stem+".lora_b"]
		if len(a) < 2 || len(b) < 2 {
			continue
		}
		// GGUF lists the fastest-changing dimension first: lora_a is
		// [in, rank] and lora_b is [rank, out].
		total += a[0] * b[1] * 2
	}
	return total
}

type ggufReader struct {
	r   *bufio.Reader
	err error
}

func (g *ggufReader) read(v any) {
	if g.err == nil {
		g.err = binary.Read(g.r, binary.LittleEndian, v)
	}
}

func (g *ggufReader) u32() uint32 {
	var v uint32
	g.read(&v)
	return v
}

func (g *ggufReader) u64() uint64 {
	var v uint64
	g.read(&v)
	return v
}

func (g *ggufReader) skip(n uint64) {
	if g.err == nil {
		_, g.err = io.CopyN(io.Discard, g.r, int64(n))
	}
}

func (g *ggufReader) str() string {
	n := g.u64()
	if g.err != nil {
		return ""
	}
	if n > 1<<24 {
		g.err = fmt.Errorf("string of %d bytes", n)
		return ""
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(g.r, b); err != nil {
		g.err = err
	}
	return string(b)
}

// GGUF metadata value types.
const (
	ggufU8, ggufI8, ggufU16, ggufI16, ggufU32, ggufI32, ggufF32, ggufBool, ggufString, ggufArray, ggufU64, ggufI64, ggufF64 = 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12
)

func (g *ggufReader) skipValue(typ uint32, depth int) {
	switch typ {
	case ggufU8, ggufI8, ggufBool:
		g.skip(1)
	case ggufU16, ggufI16:
		g.skip(2)
	case ggufU32, ggufI32, ggufF32:
		g.skip(4)
	case ggufU64, ggufI64, ggufF64:
		g.skip(8)
	case ggufString:
		g.skip(g.u64())
	case ggufArray:
		if depth > 2 {
			g.err = fmt.Errorf("nested GGUF arrays")
			return
		}
		elem, n := g.u32(), g.u64()
		for i := uint64(0); i < n && g.err == nil; i++ {
			g.skipValue(elem, depth+1)
		}
	default:
		if g.err == nil {
			g.err = fmt.Errorf("unknown GGUF value type %d", typ)
		}
	}
}
