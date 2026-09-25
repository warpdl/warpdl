//go:build linux

package warplib

import (
	"errors"

	"golang.org/x/sys/unix"
)

// copyFileRange is unix.CopyFileRange, replaceable in tests.
var copyFileRange = unix.CopyFileRange

// compileInKernel copies the part file into the main file with
// copy_file_range, so the data never passes through user space and
// filesystems with reflink support (Btrfs, XFS) share blocks instead of
// copying them. Offsets are explicit, so parts compiling concurrently into
// the same main file do not disturb each other. handled is false, with
// nothing copied, when the kernel or filesystem cannot perform the copy and
// the portable loop must be used instead.
func (p *Part) compileInKernel() (read, written int64, handled bool, err error) {
	srcFD := int(p.pf.Fd())
	dstFD := int(p.f.Fd())
	var srcOff int64
	dstOff := p.offset
	for {
		n, copyErr := copyFileRange(srcFD, &srcOff, dstFD, &dstOff, int(compileBufferSize), 0)
		if errors.Is(copyErr, unix.EINTR) {
			continue
		}
		if copyErr != nil {
			if read == 0 {
				// Unsupported (ENOSYS, EXDEV, EOPNOTSUPP, ...): nothing
				// was written yet.
				return 0, 0, false, nil
			}
			return read, written, true, copyErr
		}
		if n == 0 {
			return read, written, true, nil
		}
		read += int64(n)
		written += int64(n)
		if p.cfunc != nil {
			p.callCompileProgress(n)
		}
	}
}
