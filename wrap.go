package main

import (
	"bytes"
)

func issep(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n'
}

func nextWord(text []byte) (word []byte, remaining []byte) {
	i, max := 0, len(text)
	for i < max && issep(text[i]) {
		i++
	}
	if i > 0 {
		text = text[i:]
		max -= i
		i = 0
	}
	for i < max && !issep(text[i]) {
		i++
	}
	word = text[:i]
	for i < max && issep(text[i]) {
		i++
	}
	remaining = text[i:]
	return
}

func wrap(text []byte, prefix []byte, max int) []byte {
	var b bytes.Buffer
	var word []byte
	word, text = nextWord(text)
	b.Write(prefix)
	b.Write(word)
	offset := len(prefix) + len(word)
	for len(text) > 0 {
		word, text = nextWord(text)
		if offset+1+len(word) <= max {
			b.WriteRune(' ')
			b.Write(word)
			offset += 1 + len(word)
		} else {
			b.WriteRune('\n')
			b.Write(prefix)
			b.Write(word)
			offset = len(prefix) + len(word)
		}
	}
	return b.Bytes()
}
