package netdiag

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Result holds timing phases & response metadata for one HTTP request.
type Result struct {
	// Raw phase timestamps (zero if phase not reached).
	Start               time.Time
	DNSStart, DNSDone   time.Time
	ConnStart, ConnDone time.Time
	TLSStart, TLSDone   time.Time
	FirstByte           time.Time
	End                 time.Time

	// Outcome
	Method     string
	URL        string
	Status     string
	Proto      string
	Err        error
	RespHeader http.Header
	TLSState   *tls.ConnectionState
	// Whether the underlying connection was reused.
	ReusedConn bool
}

// Durations (computed lazily so callers can format however they want)
func (r *Result) DNSDuration() time.Duration     { return dur(r.DNSStart, r.DNSDone) }
func (r *Result) ConnectDuration() time.Duration { return dur(r.ConnStart, r.ConnDone) }
func (r *Result) TLSDuration() time.Duration     { return dur(r.TLSStart, r.TLSDone) }
func (r *Result) TTFB() time.Duration            { return dur(r.Start, r.FirstByte) }
func (r *Result) Total() time.Duration           { return dur(r.Start, r.effectiveEnd()) }
func (r *Result) effectiveEnd() time.Time {
	if !r.End.IsZero() {
		return r.End
	}
	if !r.FirstByte.IsZero() {
		return r.FirstByte
	}
	if !r.ConnDone.IsZero() {
		return r.ConnDone
	}
	return r.Start
}
func dur(a, b time.Time) time.Duration {
	if a.IsZero() || b.IsZero() {
		return 0
	}
	return b.Sub(a)
}

// Diagnose executes a single HTTP request (default HEAD) and returns a populated Result.
// method="" defaults to HEAD; pass GET if you want body download to influence Total.
// ctx controls total timeout / cancellation *outside* the per-request http.Client timeout you might add.
func Diagnose(ctx context.Context, rawURL string, method string, client *http.Client) (*Result, error) {
	if method == "" {
		method = "HEAD"
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Method: method,
		URL:    rawURL,
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}

	// Timestamps via httptrace
	res.Start = time.Now()
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { res.DNSStart = time.Now() },
		DNSDone:           func(httptrace.DNSDoneInfo) { res.DNSDone = time.Now() },
		ConnectStart:      func(_, _ string) { res.ConnStart = time.Now() },
		ConnectDone:       func(_, _ string, _ error) { res.ConnDone = time.Now() },
		TLSHandshakeStart: func() { res.TLSStart = time.Now() },
		TLSHandshakeDone: func(cs tls.ConnectionState, _ error) {
			res.TLSDone = time.Now()
			// Capture by pointer to allow nil when not TLS.
			copy := cs
			res.TLSState = &copy
		},
		GotConn: func(info httptrace.GotConnInfo) {
			res.ReusedConn = info.Reused
		},
		GotFirstResponseByte: func() { res.FirstByte = time.Now() },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err != nil && res.Err == nil {
				res.Err = info.Err
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	// Provide a default client if none supplied.
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true, // mirror fresh-connection behavior (can be overridden by caller)
			},
			Timeout: 0, // rely on ctx if you want timeouts; or set here.
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		res.Err = err
		// End timestamp before return so Total() isn't zero.
		res.End = time.Now()
		return res, err
	}
	res.Proto = resp.Proto
	res.Status = resp.Status
	res.RespHeader = resp.Header.Clone()
	// Drain (or discard) body for accurate total timing; for HEAD it is empty.
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	res.End = time.Now()
	return res, nil
}

func (r *Result) StringVerbose() string {
	var buf strings.Builder
	r.WriteVerbose(&buf)
	return buf.String()
}

// WriteVerbose writes a curl -v style transcript (phases + headers + timings) to w.
func (r *Result) WriteVerbose(w io.Writer) error {
	bw := func(format string, a ...any) {
		fmt.Fprintf(w, format, a...)
	}

	host := hostOnly(r.URL)
	bw("*   Trying %s ...\n", host)

	// DNS
	if r.DNSDuration() > 0 {
		bw("* DNS resolved %s in %v\n", host, r.DNSDuration())
	}

	// Connect
	if r.ConnectDuration() > 0 {
		bw("* Connected in %v (reused=%v)\n", r.ConnectDuration(), r.ReusedConn)
	}

	// TLS
	if r.TLSDuration() > 0 {
		if r.TLSState != nil {
			bw("* TLS handshake in %v (version=0x%x cipher=0x%x alpn=%s)\n",
				r.TLSDuration(), r.TLSState.Version, r.TLSState.CipherSuite,
				printable(r.TLSState.NegotiatedProtocol))
		} else {
			bw("* TLS handshake in %v\n", r.TLSDuration())
		}
	} else {
		bw("* (no TLS)\n")
	}

	// Outgoing request line (we know what we sent)
	u, _ := url.Parse(r.URL)
	bw("> %s %s HTTP/1.1\n", r.Method, u.RequestURI())
	bw("> Host: %s\n", u.Host)
	bw("> User-Agent: netdiag/1.0\n")
	bw("> Accept: */*\n")
	bw(">\n")

	// Response or error
	if r.Err != nil {
		bw("! error: %v\n", r.Err)
	} else {
		bw("< %s %s\n", r.Proto, r.Status)
		writeSortedHeaders(w, r.RespHeader)
		bw("\n")
	}

	r.WriteTimings(w)
	return nil
}

// WriteTimings writes only the curl -w style timing summary.
func (r *Result) WriteTimings(w io.Writer) {
	fmt.Fprintln(w, "== Timings (curl style) ==")
	fmt.Fprintf(w, "time_namelookup:   %10.3f ms\n", ms(r.DNSDuration()))
	fmt.Fprintf(w, "time_connect:      %10.3f ms\n", ms(r.ConnectDuration()))
	fmt.Fprintf(w, "time_appconnect:   %10.3f ms\n", ms(r.TLSDuration()))
	fmt.Fprintf(w, "time_starttransfer:%10.3f ms\n", ms(r.TTFB()))
	fmt.Fprintf(w, "time_total:        %10.3f ms\n", ms(r.Total()))
	if r.Err != nil {
		fmt.Fprintln(w, "(partial—error encountered)")
	}
}

// Implement io.WriterTo so callers can: io.Copy(os.Stdout, res)
func (r *Result) WriteTo(w io.Writer) (int64, error) {
	var buf strings.Builder
	r.WriteVerbose(&buf)
	nWritten, err := io.WriteString(w, buf.String())
	return int64(nWritten), err
}

// Helpers
func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000.0 }
func printable(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
func hostOnly(raw string) string {
	withoutScheme := raw
	if i := strings.Index(raw, "://"); i >= 0 {
		withoutScheme = raw[i+3:]
	}
	if j := strings.IndexAny(withoutScheme, "/?#"); j >= 0 {
		withoutScheme = withoutScheme[:j]
	}
	return withoutScheme
}
func writeSortedHeaders(w io.Writer, h http.Header) {
	// Order for stability (similar to how curl prints as they arrive; we just sort)
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	textproto.CanonicalMIMEHeaderKey("")
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range h[k] {
			fmt.Fprintf(w, "%s: %s\n", k, v)
		}
	}
}
