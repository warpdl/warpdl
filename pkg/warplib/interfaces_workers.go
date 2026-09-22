package warplib

import (
	"fmt"
	"runtime/debug"
	"sort"
	"sync/atomic"
)

type bondedJob struct {
	part *Part
	foff int64
}

// startBonded registers every part before launching workers, then lets those
// workers pull from one queue. The caller waits on d.wg.
func (d *Downloader) startBonded(resumeParts map[int64]*ItemPart) error {
	jobs, err := d.prepareBondedJobs(resumeParts)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	workers := d.bondedWorkerCount()
	if workers < 1 {
		for _, job := range jobs {
			job.part.close()
		}
		return fmt.Errorf("%w: no interface workers", ErrInvalidMaxConnections)
	}
	d.liveWorkers.Store(int32(workers))
	d.refreshSpeedShares()
	jobCh := make(chan bondedJob, len(jobs))
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)
	d.wg.Add(workers)
	for i := 0; i < workers; i++ {
		index := i % len(d.ifaceClients)
		go d.bondedWorker(d.ifaceClients[index], index, jobCh)
	}
	return nil
}

func (d *Downloader) prepareBondedJobs(resumeParts map[int64]*ItemPart) ([]bondedJob, error) {
	if resumeParts == nil {
		return d.spawnBondedJobs()
	}
	return d.resumeBondedJobs(resumeParts)
}

func (d *Downloader) spawnBondedJobs() ([]bondedJob, error) {
	partSize, remainder := d.getPartSize()
	if partSize <= 0 {
		return nil, fmt.Errorf("%w: bonded part size %d", ErrContentLengthInvalid, partSize)
	}
	jobs := make([]bondedJob, 0, d.numBaseParts)
	for i := int32(0); i < d.numBaseParts; i++ {
		ioff := int64(i) * partSize
		foff := ioff + partSize - 1
		if i == d.numBaseParts-1 {
			foff += remainder
		}
		part, err := d.spawnPart(ioff, foff)
		if err != nil {
			for _, job := range jobs {
				job.part.close()
			}
			return nil, err
		}
		jobs = append(jobs, bondedJob{part: part, foff: foff})
	}
	return jobs, nil
}

func (d *Downloader) resumeBondedJobs(resumeParts map[int64]*ItemPart) ([]bondedJob, error) {
	starts := make([]int64, 0, len(resumeParts))
	for start := range resumeParts {
		starts = append(starts, start)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	jobs := make([]bondedJob, 0, len(starts))
	for _, ioff := range starts {
		ip := resumeParts[ioff]
		if ip == nil {
			for _, job := range jobs {
				job.part.close()
			}
			return nil, fmt.Errorf("%w: nil part at offset %d", ErrItemPartNil, ioff)
		}
		if ip.Compiled {
			partLength := ip.FinalOffset - ioff + 1
			d.handlers.CompileSkippedHandler(ip.Hash, partLength)
			atomic.AddInt64(&d.nread, partLength)
			continue
		}
		part, err := d.initPart(ip.Hash, ioff, ip.FinalOffset)
		if err != nil {
			for _, job := range jobs {
				job.part.close()
			}
			return nil, err
		}
		expected := ip.FinalOffset - ioff + 1
		persisted := part.getRead()
		if persisted > expected {
			part.close()
			for _, job := range jobs {
				job.part.close()
			}
			return nil, fmt.Errorf("%w: persisted part %s contains %d bytes, declared range requires %d",
				ErrDownloadSizeMismatch, ip.Hash, persisted, expected)
		}
		if persisted < expected && d.resourceETag == "" {
			part.close()
			for _, job := range jobs {
				job.part.close()
			}
			return nil, fmt.Errorf("%w: cannot append to persisted part %s without a strong ETag",
				ErrResourceChanged, ip.Hash)
		}
		if persisted == expected {
			if err := d.compileBondedPart(part); err != nil {
				part.close()
				for _, job := range jobs {
					job.part.close()
				}
				return nil, err
			}
			part.close()
			continue
		}
		jobs = append(jobs, bondedJob{part: part, foff: ip.FinalOffset})
	}
	return jobs, nil
}

func (d *Downloader) bondedWorker(slot ifaceClient, index int, jobs <-chan bondedJob) {
	defer d.wg.Done()
	defer func() {
		if d.liveWorkers.Add(-1) < 0 {
			d.liveWorkers.Store(0)
		}
		d.refreshSpeedShares()
	}()
	defer func() {
		if recovered := recover(); recovered != nil {
			if d.l != nil {
				d.l.Printf("PANIC in bondedWorker: %v\n%s", recovered, debug.Stack())
			}
			d.failWorker(slot.name, fmt.Errorf("panic: %v", recovered))
		}
	}()
	for job := range jobs {
		d.runBondedJob(slot, index, job)
	}
}

func (d *Downloader) runBondedJob(slot ifaceClient, index int, job bondedJob) {
	part := job.part
	defer part.close()
	if atomic.LoadInt32(&d.stopped) == 1 || (d.ctx != nil && d.ctx.Err() != nil) {
		return
	}
	part.client = slot.client
	d.assignPartInterface(part.hash, index)
	atomic.AddInt32(&d.numConn, 1)
	defer atomic.AddInt32(&d.numConn, -1)
	part.applySpeedLimit(d.currentPartSpeedLimit())
	err := d.runPart(part, part.offset+part.getRead(), job.foff, 4*MB, false, nil)
	if err != nil {
		d.storeWorkerError(part.hash, err)
		return
	}
	if err := d.compileBondedPart(part); err != nil {
		d.failWorker(part.hash, err)
	}
}

func (d *Downloader) compileBondedPart(part *Part) error {
	d.handlers.CompileStartHandler(part.hash)
	readCapture := part.getRead()
	d.Log("%s: compiling part", part.hash)
	read, written, err := part.compileExact(readCapture)
	if err != nil {
		return fmt.Errorf("compile part: %w", err)
	}
	atomic.AddInt64(&d.nread, written)
	d.handlers.CompileCompleteHandler(part.hash, readCapture)
	d.Log("%s: compilation complete: read %d bytes and wrote %d bytes", part.hash, read, written)
	if err := WarpRemove(getFileName(d.dlPath, part.hash)); err != nil {
		d.Log("%s: remove: %v", part.hash, err)
	}
	return nil
}
