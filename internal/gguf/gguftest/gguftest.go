// Package gguftest writes tiny GGUF files for tests: a header with a few
// values and one small tensor, laid out as a real model's is.
package gguftest

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// Options shape the file.
type Options struct {
	// Name, Architecture, and Type are general.name, general.architecture,
	// and general.type; Architecture defaults to llama.
	Name, Architecture, Type string
	// FileType is general.file_type (15 is Q4_K_M); 0 leaves it out.
	FileType uint32
	// Context is {arch}.context_length; 0 leaves it out.
	Context uint32
	// Elements is the tensor's F32 element count (default 64).
	Elements uint64
	// Cut drops this many bytes from the end, as an interrupted copy would.
	Cut int
	// Vocab is tokenizer.ggml.tokens, and Control which of them are control
	// tokens (tokenizer.ggml.token_type 3; the rest are 1, normal).
	Vocab   []string
	Control map[string]bool
	// ChatTemplate is tokenizer.chat_template; empty leaves it out.
	ChatTemplate string
}

// Bytes is the file's content.
func Bytes(o Options) []byte {
	if o.Architecture == "" {
		o.Architecture = "llama"
	}
	if o.Elements == 0 {
		o.Elements = 64
	}
	var b bytes.Buffer
	u32 := func(v uint32) { _ = binary.Write(&b, binary.LittleEndian, v) }
	u64 := func(v uint64) { _ = binary.Write(&b, binary.LittleEndian, v) }
	str := func(s string) { u64(uint64(len(s))); b.WriteString(s) }
	const typeUint32, typeInt32, typeString, typeArray = 4, 5, 8, 9
	var kv bytes.Buffer
	n := 0
	put := func(key string, write func()) {
		// Values are written after the counts, so collect them first.
		saved := b
		b = kv
		str(key)
		write()
		kv = b
		b = saved
		n++
	}
	put("general.architecture", func() { u32(typeString); str(o.Architecture) })
	if o.Name != "" {
		put("general.name", func() { u32(typeString); str(o.Name) })
	}
	if o.Type != "" {
		put("general.type", func() { u32(typeString); str(o.Type) })
	}
	if o.FileType != 0 {
		put("general.file_type", func() { u32(typeUint32); u32(o.FileType) })
	}
	if o.Context != 0 {
		put(o.Architecture+".context_length", func() { u32(typeUint32); u32(o.Context) })
	}
	if o.ChatTemplate != "" {
		put("tokenizer.chat_template", func() { u32(typeString); str(o.ChatTemplate) })
	}
	if len(o.Vocab) > 0 {
		put("tokenizer.ggml.tokens", func() {
			u32(typeArray)
			u32(typeString)
			u64(uint64(len(o.Vocab)))
			for _, tok := range o.Vocab {
				str(tok)
			}
		})
		put("tokenizer.ggml.token_type", func() {
			u32(typeArray)
			u32(typeInt32)
			u64(uint64(len(o.Vocab)))
			for _, tok := range o.Vocab {
				if o.Control[tok] {
					u32(3)
				} else {
					u32(1)
				}
			}
		})
	}
	b.WriteString("GGUF")
	u32(3)
	u64(1) // one tensor
	u64(uint64(n))
	b.Write(kv.Bytes())
	str("token_embd.weight")
	u32(1)
	u64(o.Elements)
	u32(0) // F32
	u64(0) // offset into the data
	for b.Len()%32 != 0 {
		b.WriteByte(0)
	}
	b.Write(make([]byte, o.Elements*4))
	out := b.Bytes()
	if o.Cut > 0 && o.Cut < len(out) {
		out = out[:len(out)-o.Cut]
	}
	return out
}

// Write saves a file named name in dir and returns its path.
func Write(t testing.TB, dir, name string, o Options) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, Bytes(o), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
