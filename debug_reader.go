package main

import (
	"fmt"
	"io"
	"os"
)

type debugReader struct {
	r io.Reader
}

func (d debugReader) Read(p []byte) (n int, err error) {
	n, err = d.r.Read(p)
	if n > 0 {
		f, logErr := os.OpenFile("input_bytes.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if logErr == nil {
			defer f.Close()
			fmt.Fprintf(f, "Read %d bytes: %q\n", n, p[:n])
		}
	}
	return
}
