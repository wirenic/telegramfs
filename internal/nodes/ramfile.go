package nodes

import (
	"io"
)

// RAMFile is an in-memory implementation of io.ReaderAt and io.WriterAt, and
// of srv.FReadOp and srv.FWriteOp.
type RAMFile struct {
	buffer []byte
	off    int64
}

// NewRAMFile creates a RAMFile with the given initial contents.
//
// It does not retain the passed slice.
func NewRAMFile(contents []byte) *RAMFile {
	var f RAMFile
	f.buffer = make([]byte, len(contents))
	copy(f.buffer, contents)
	return &f
}

func (f *RAMFile) Truncate() {
	f.buffer = nil
	f.off = 0
}

func (f *RAMFile) Read(p []byte) (n int, err error) {
	n, err = f.ReadAt(p, f.off)
	f.off += int64(n)
	return
}

// ReadAt implements io.ReaderAt.
func (f *RAMFile) ReadAt(p []byte, off int64) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off >= len64(f.buffer) {
		return 0, io.EOF
	}
	n = copy(p, f.buffer[off:])
	if n < len(p) {
		err = io.EOF
	}
	return
}

// WriteAt implements io.WriterAt.
func (f *RAMFile) WriteAt(p []byte, off int64) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off > len64(f.buffer) {
		larger := make([]byte, off+len64(p))
		copy(larger, f.buffer)
		f.buffer = larger
	}
	if n := copy(f.buffer[off:], p); n < len(p) {
		f.buffer = append(f.buffer, p[n:]...)
	}
	return len(p), nil
}

func len64(p []byte) int64 {
	return int64(len(p))
}
