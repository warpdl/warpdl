package warplib

import "strings"

type ContentLength int64

func (c ContentLength) v() (clen int64) {
	return int64(c)
}

func (c ContentLength) String() (clen string) {
	clen = c.Format(
		" ",
		SizeOptionTB,
		SizeOptionGB,
		SizeOptionMB,
		SizeOptionKB,
	)
	if clen == "" {
		clen = "undefined"
	}
	return
}

func (c ContentLength) Format(sep string, sizeOpts ...SizeOption) (clen string) {
	b := strings.Builder{}
	n := len(sizeOpts) - 1
	for i, opt := range sizeOpts {
		siz, rem := opt.Get(c)
		c = ContentLength(rem)
		if siz == 0 {
			continue
		}
		fl := opt.StringFrom(siz)
		b.WriteString(fl)
		if i == n {
			break
		}
		b.WriteString(sep)
	}
	clen = b.String()
	return
}

func (c *ContentLength) IsUnknown() (unknown bool) {
	return c.v() == -1
}
