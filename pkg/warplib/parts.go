package warplib

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Part struct {
	ctx context.Context
	// URL
	url string
	// size of a bytes chunk to be used for copying
	chunk int
	// unique hash for this part
	hash string
	// number of bytes downloaded
	read int64
	// download progress handler
	pfunc DownloadProgressHandlerFunc
	// download complete handler
	ofunc DownloadCompleteHandlerFunc
	// compile progress handler
	cfunc CompileProgressHandlerFunc
	// http client
	client *http.Client
	// prename
	preName string
	// part file
	pf *os.File
	// offset of part
	offset int64
	// expected speed
	etime time.Duration
	// logger
	l  *log.Logger
	wg *sync.WaitGroup
	// main download file
	f *os.File
}

type partArgs struct {
	copyChunk int
	preName   string
	rpHandler ResumeProgressHandlerFunc
	pHandler  DownloadProgressHandlerFunc
	oHandler  DownloadCompleteHandlerFunc
	cpHandler CompileProgressHandlerFunc
	logger    *log.Logger
	offset    int64
	f         *os.File
}

func initPart(ctx context.Context, wg *sync.WaitGroup, client *http.Client, hash, url string, args partArgs) (*Part, error) {
	p := Part{
		ctx:     ctx,
		url:     url,
		client:  client,
		chunk:   args.copyChunk,
		preName: args.preName,
		pfunc:   args.pHandler,
		ofunc:   args.oHandler,
		cfunc:   args.cpHandler,
		l:       args.logger,
		offset:  args.offset,
		hash:    hash,
		wg:      wg,
		f:       args.f,
	}
	err := p.openPartFile()
	if err != nil {
		return nil, err
	}
	err = p.seek(args.rpHandler)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func newPart(ctx context.Context, wg *sync.WaitGroup, client *http.Client, url string, args partArgs) (*Part, error) {
	p := Part{
		ctx:     ctx,
		url:     url,
		client:  client,
		chunk:   args.copyChunk,
		preName: args.preName,
		pfunc:   args.pHandler,
		ofunc:   args.oHandler,
		cfunc:   args.cpHandler,
		l:       args.logger,
		offset:  args.offset,
		wg:      wg,
		f:       args.f,
	}
	p.setHash()
	return &p, p.createPartFile()
}

func (p *Part) setEpeed(espeed int64) {
	p.etime = getDownloadTime(espeed, int64(p.chunk))
}

func (p *Part) download(headers Headers, ioff, foff int64, force bool) (slow bool, err error) {
	req, er := http.NewRequestWithContext(p.ctx, http.MethodGet, p.url, nil)
	if er != nil {
		err = er
		return
	}
	header := req.Header
	headers.Set(header)
	if foff != -1 {
		setRange(header, ioff, foff)
	} else {
		force = true
	}
	resp, er := p.client.Do(req)
	if er != nil {
		err = er
		return
	}
	defer resp.Body.Close()
	return p.copyBuffer(resp.Body, p.pf, force)
}

func (p *Part) copyBuffer(src io.Reader, dst io.Writer, force bool) (slow bool, err error) {
	var (
		te  time.Duration
		buf = make([]byte, p.chunk)
	)
	fmt.Println("forcing download", force)
	var n int
	for {
		n++
		if !force && n%10 == 0 {
			te, err = getSpeed(func() error {
				return p.copyBufferChunk(src, dst, buf)
			})
			if err != nil {
				break
			}
			if te > p.etime {
				slow = true
				return
			}
			continue
		}
		err = p.copyBufferChunk(src, dst, buf)
		if err != nil {
			break
		}
	}
	if err == io.EOF {
		err = nil
		p.log("%s: part download complete", p.hash)
		p.wg.Add(1)
		go func() {
			p.ofunc(p.hash, p.read)
			p.wg.Done()
		}()
	}
	return
}

func (p *Part) copyBufferChunk(src io.Reader, dst io.Writer, buf []byte) (err error) {
	nr, er := src.Read(buf)
	if nr > 0 {
		nw, ew := dst.Write(buf[0:nr])
		if nw < 0 || nr < nw {
			nw = 0
			if ew == nil {
				ew = errors.New("invalid write results")
			}
		}
		p.read += int64(nw)
		p.wg.Add(1)
		go func() {
			p.pfunc(p.hash, nw)
			p.wg.Done()
		}()
		if ew != nil {
			err = ew
			return
		}
		if nr != nw {
			err = io.ErrShortWrite
			return
		}
	}
	err = er
	return
}

func (p *Part) compile() (read, written int64, err error) {
	wg := &sync.WaitGroup{}
	// take the reader to origin from end
	p.pf.Seek(0, 0)

	buf := make([]byte, p.chunk)
	off := p.offset
	for {
		nr, er := p.pf.Read(buf)
		read += int64(nr)
		if nr > 0 {
			nw, ew := p.f.WriteAt(buf[0:nr], off)
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = errors.New("invalid write results")
				}
			}
			written += int64(nw)
			wg.Add(1)
			go func() {
				p.cfunc(p.hash, nw)
				wg.Done()
			}()
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		off += int64(nr)
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	wg.Wait()
	return
}

func setRange(header http.Header, ioff, foff int64) {
	str := func(i int64) string {
		return strconv.FormatInt(i, 10)
	}
	var b strings.Builder
	b.WriteString("bytes=")
	b.WriteString(str(ioff))
	b.WriteRune('-')
	if foff != 0 {
		b.WriteString(str(foff))
	}
	header.Set("Range", b.String())
}

func (p *Part) setHash() {
	t := make([]byte, 2)
	rand.Read(t)
	p.hash = hex.EncodeToString(t)
}

func (p *Part) createPartFile() (err error) {
	p.pf, err = os.Create(p.getFileName())
	return
}

func (p *Part) openPartFile() (err error) {
	p.pf, err = os.OpenFile(p.getFileName(), os.O_RDWR, 0666)
	return
}

func (p *Part) seek(rpFunc ResumeProgressHandlerFunc) (err error) {
	pReader := NewProxyReader(p.pf, func(n int) {
		rpFunc(p.hash, n)
	})
	n, err := io.Copy(io.Discard, pReader)
	if err != nil {
		return
	}
	p.read = n
	return
}

func (p *Part) getFileName() string {
	return getFileName(p.preName, p.hash)
}

func (p *Part) close() error {
	return p.pf.Close()
}

func (p *Part) log(s string, a ...any) {
	p.l.Printf(s+"\n", a...)
}

func (p *Part) String() string {
	return p.hash
}
