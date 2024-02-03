package warplib

import "io"

type ProxyReader struct {
	r io.Reader
	c func(n int)
}

func NewProxyReader(reader io.Reader, callback func(n int)) *ProxyReader {
	return &ProxyReader{
		r: reader,
		c: callback,
	}
}

func (p *ProxyReader) Read(b []byte) (n int, err error) {
	n, err = p.r.Read(b)
	go p.c(n)
	return
}
