//go:build !linux

package warplib

// compileInKernel has no portable kernel-side copy with explicit offsets, so
// compile always uses the read/write loop on this platform.
func (p *Part) compileInKernel() (read, written int64, handled bool, err error) {
	return 0, 0, false, nil
}
