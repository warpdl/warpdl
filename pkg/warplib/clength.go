package warplib

import "strings"

// ContentLength represents the size of a download item.
// It is used to store the total size of the download item
// and the amount of data that has been downloaded.
type ContentLength int64

// v returns the value of the ContentLength as an int64.
func (c ContentLength) v() (clen int64) {
	return int64(c)
}

// String returns the string representation of the ContentLength.
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

// Format returns the formatted string representation of the ContentLength.
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

// IsUnknown returns whether the ContentLength is unknown.
func (c *ContentLength) IsUnknown() (unknown bool) {
	return c.v() == -1
}
