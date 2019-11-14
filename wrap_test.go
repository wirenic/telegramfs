package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNextWord(t *testing.T) {
	text := []byte(`Lorem ipsum dolor sit amet, consectetur adipiscing elit,
sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`)
	var word []byte
	word, text = nextWord(text)
	if got, want := string(word), "Lorem"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "ipsum"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "dolor"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "sit"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "amet,"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "consectetur"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "adipiscing"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "elit,"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "sed"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "do"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "eiusmod"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "tempor"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "incididunt"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "ut"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "labore"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "et"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "dolore"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "magna"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "aliqua."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(text) != 0 {
		t.Fatalf("got %q, want nil", text)
	}
}

func TestNextWordEdgeCases(t *testing.T) {
	text := []byte(` With   many
	separators.
	`)
	var word []byte
	word, text = nextWord(text)
	if got, want := string(word), "With"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "many"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	word, text = nextWord(text)
	if got, want := string(word), "separators."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(text) != 0 {
		t.Fatalf("got %q, want nil", text)
	}
}

func TestWrapNoPrefix(t *testing.T) {
	text := []byte(`Lorem ipsum dolor sit amet, consectetur adipiscing elit,
sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`)
	wrapped := wrap(text, nil, 40)
	want := []byte(`Lorem ipsum dolor sit amet, consectetur
adipiscing elit, sed do eiusmod tempor
incididunt ut labore et dolore magna
aliqua.`)
	if d := cmp.Diff(want, wrapped); d != "" {
		t.Fatal(d)
	}
	wrapped = wrap(text, nil, 20)
	want = []byte(`Lorem ipsum dolor
sit amet,
consectetur
adipiscing elit, sed
do eiusmod tempor
incididunt ut labore
et dolore magna
aliqua.`)
	if d := cmp.Diff(want, wrapped); d != "" {
		t.Fatal(d)
	}
	wrapped = wrap(text, nil, 10)
	want = []byte(`Lorem
ipsum
dolor sit
amet,
consectetur
adipiscing
elit, sed
do eiusmod
tempor
incididunt
ut labore
et dolore
magna
aliqua.`)
	if d := cmp.Diff(want, wrapped); d != "" {
		t.Fatal(d)
	}
	wrapped = wrap(text, nil, 1)
	want = []byte(`Lorem
ipsum
dolor
sit
amet,
consectetur
adipiscing
elit,
sed
do
eiusmod
tempor
incididunt
ut
labore
et
dolore
magna
aliqua.`)
	if d := cmp.Diff(want, wrapped); d != "" {
		t.Fatal(d)
	}
}

func TestWrapPrefix(t *testing.T) {
	prefix := []byte("> ")
	text := []byte(`Lorem ipsum dolor sit amet, consectetur adipiscing elit,
sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`)
	wrapped := wrap(text, prefix, 40)
	want := []byte(`> Lorem ipsum dolor sit amet,
> consectetur adipiscing elit, sed do
> eiusmod tempor incididunt ut labore et
> dolore magna aliqua.`)
	if d := cmp.Diff(want, wrapped); d != "" {
		t.Fatal(d)
	}
	wrapped = wrap(text, prefix, 20)
	want = []byte(`> Lorem ipsum dolor
> sit amet,
> consectetur
> adipiscing elit,
> sed do eiusmod
> tempor incididunt
> ut labore et
> dolore magna
> aliqua.`)
	if d := cmp.Diff(want, wrapped); d != "" {
		t.Fatal(d)
	}
	wrapped = wrap(text, prefix, 10)
	want = []byte(`> Lorem
> ipsum
> dolor
> sit
> amet,
> consectetur
> adipiscing
> elit,
> sed do
> eiusmod
> tempor
> incididunt
> ut
> labore
> et
> dolore
> magna
> aliqua.`)
	if d := cmp.Diff(want, wrapped); d != "" {
		t.Fatal(d)
	}
}
