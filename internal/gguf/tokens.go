package gguf

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// tokenControl is a vocabulary entry's type for a control token, such as
// <|im_start|> or <start_of_turn>: one a chat template uses, never text.
const tokenControl = 3

// ControlTokens reads a model's control tokens from its vocabulary, as
// tokenizer.ggml.tokens and tokenizer.ggml.token_type list them (#829).
// llama-server parses these spellings in a prompt as the tokens, so text
// that spells one becomes, say, a real turn marker. Only spellings that
// can't be ordinary words are kept: they hold something other than letters
// and digits.
func ControlTokens(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d := decoder{r: bufio.NewReaderSize(f, 1<<16)}
	if magic := d.bytes(4); string(magic) != "GGUF" {
		if d.err != nil {
			return nil, d.err
		}
		return nil, errors.New("not a GGUF file")
	}
	if version := d.u32(); version < 2 {
		return nil, fmt.Errorf("GGUF version %d is not supported", version)
	}
	d.u64() // tensor count
	count := d.u64()
	var tokens []string
	var types []uint64
	for n := uint64(0); n < count && d.err == nil; n++ {
		key := d.str()
		typ := d.u32()
		switch {
		case key == "tokenizer.ggml.tokens" && typ == typeArray:
			tokens = d.strings()
		case key == "tokenizer.ggml.token_type" && typ == typeArray:
			types = d.numbers()
		default:
			d.skip(typ, 0)
		}
		if tokens != nil && types != nil {
			break
		}
	}
	if d.err != nil {
		return nil, d.err
	}
	var out []string
	for i, tok := range tokens {
		if i < len(types) && types[i] == tokenControl && notAWord(tok) {
			out = append(out, tok)
		}
	}
	return out, nil
}

func notAWord(tok string) bool {
	return len(tok) >= 3 && strings.ContainsFunc(tok, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_')
	})
}

// strings reads an array of strings, after its value type.
func (d *decoder) strings() []string {
	elem := d.u32()
	n := d.u64()
	if elem != typeString {
		d.skipElements(elem, n)
		return []string{}
	}
	out := make([]string, 0, min(n, 1<<20))
	for i := uint64(0); i < n && d.err == nil; i++ {
		out = append(out, d.str())
	}
	return out
}

// numbers reads an array of integers, after its value type.
func (d *decoder) numbers() []uint64 {
	elem := d.u32()
	n := d.u64()
	out := make([]uint64, 0, min(n, 1<<20))
	for i := uint64(0); i < n && d.err == nil; i++ {
		v, ok := d.number(elem)
		if !ok {
			return []uint64{}
		}
		out = append(out, v)
	}
	return out
}

func (d *decoder) skipElements(elem uint32, n uint64) {
	if size := fixedSize(elem); size > 0 {
		d.discard(n * size)
		return
	}
	for i := uint64(0); i < n && d.err == nil; i++ {
		d.skip(elem, 1)
	}
}
