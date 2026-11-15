//go:build !integration

package atif_test

type repeatingReader struct {
	pattern []byte
	offset  int
}

func (r *repeatingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.pattern[r.offset]
		r.offset = (r.offset + 1) % len(r.pattern)
	}
	return len(p), nil
}
