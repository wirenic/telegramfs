package nodes

import (
	"io/ioutil"
	"math/rand"
	"testing"
	"time"
)

func TestRAMFile(t *testing.T) {
	// Compare system under test (sut) with reference inplementation (ref).
	// We prove that a *RAMFile is indistinguishable from *os.File.
	sut := &RAMFile{}
	ref, err := ioutil.TempFile("", "telegramfs-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ref.Close() }()
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < 100000; i++ {
		off := rand.Int63n(8192)
		size := rand.Intn(128)
		p := make([]byte, size)
		if rand.Int()%2 == 0 {
			nsut, esut := sut.ReadAt(p, off)
			nref, eref := ref.ReadAt(p, off)
			if nsut != nref {
				t.Errorf("got %d, want %d (read bytes)", nsut, nref)
			}
			if esut != eref {
				t.Errorf("got %v, want %v (read error)", esut, eref)
			}
		} else {
			rand.Read(p)
			nsut, esut := sut.WriteAt(p, off)
			nref, eref := ref.WriteAt(p, off)
			if nsut != nref {
				t.Errorf("got %d, want %d (write bytes)", nsut, nref)
			}
			if esut != eref {
				t.Errorf("got %v, want %v (write error)", esut, eref)
			}
		}
	}
}
