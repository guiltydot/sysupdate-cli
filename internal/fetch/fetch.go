// Package fetch downloads files with progress output and SHA-256 verification.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client has no overall timeout, because a large download on a slow link can
// take minutes; callers bound the download with their context instead.
var Client = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
}}

// Download fetches url into dst, printing progress to out (which may be nil),
// and verifies the SHA-256 when wantSHA is not empty. If maxBytes > 0, a larger
// response is rejected before more than maxBytes+1 bytes are written.
// It returns the number of bytes written.
func Download(ctx context.Context, out io.Writer, url, dst, wantSHA string, maxBytes int64) (int64, error) {
	if out == nil {
		out = io.Discard
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return 0, fmt.Errorf("GET %s: %d bytes, more than the %d allowed", url, resp.ContentLength, maxBytes)
	}
	var body io.Reader = resp.Body
	if maxBytes > 0 {
		body = io.LimitReader(resp.Body, maxBytes+1)
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	h := sha256.New()
	pw := &progressWriter{out: out, total: resp.ContentLength, last: -1}
	n, err := io.Copy(io.MultiWriter(f, h, pw), body)
	// A failed Close can mean the file was not fully written (disk full).
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	if pw.last >= 0 {
		fmt.Fprintln(out)
	}
	if maxBytes > 0 && n > maxBytes {
		return n, fmt.Errorf("GET %s: more than the %d bytes allowed", url, maxBytes)
	}
	if got := hex.EncodeToString(h.Sum(nil)); wantSHA != "" && got != wantSHA {
		return n, fmt.Errorf("sha256 mismatch: got %s, want %s", got, wantSHA)
	}
	return n, nil
}

type progressWriter struct {
	out   io.Writer
	total int64
	n     int64
	last  int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	if p.total > 0 {
		pct := p.n * 100 / p.total
		if pct != p.last {
			p.last = pct
			fmt.Fprintf(p.out, "\rDownloading %d%% (%.1f / %.1f MB)", pct, float64(p.n)/1e6, float64(p.total)/1e6)
		}
	}
	return len(b), nil
}
