package warplib

import (
	"strconv"
	"strings"
)

type SizeOption struct {
	val int64
	fmt string
}

func (s *SizeOption) string(l int64) string {
	return strings.Join(
		[]string{
			strconv.FormatInt(l, 10),
			s.fmt,
		}, "",
	)
}

func (s *SizeOption) Get(l ContentLength) (siz, rem int64) {
	siz = l.v() / s.val
	rem = l.v() % s.val
	return
}

func (s *SizeOption) GetFrom(l int64) (siz, rem int64) {
	siz = l / s.val
	rem = l % s.val
	return
}

func (s *SizeOption) String(l ContentLength) string {
	siz, _ := s.Get(l)
	return s.string(siz)
}

func (s *SizeOption) StringFrom(l int64) string {
	return s.string(l)
}

var (
	SizeOptionBy = SizeOption{B, "Bytes"}
	SizeOptionKB = SizeOption{KB, "KB"}
	SizeOptionMB = SizeOption{MB, "MB"}
	SizeOptionGB = SizeOption{GB, "GB"}
	SizeOptionTB = SizeOption{TB, "TB"}
)
