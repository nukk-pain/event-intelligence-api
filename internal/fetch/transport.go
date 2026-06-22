package fetch

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

func (f *Fetcher) newHTTPClient() *http.Client {
	baseDialer := &net.Dialer{Timeout: f.timeout, KeepAlive: 30 * time.Second}
	guardedDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("fetch: no addresses for %s", host)
		}
		var dialErr error
		for _, ip := range ips {
			if isMetadataOrLinkLocal(ip) {
				return nil, fmt.Errorf("%w: %s (link-local/metadata)", ErrBlockedAddress, ip)
			}
			if isBlockedIP(ip) && !(f.allowLoopopt && ip.IsLoopback()) {
				return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
			}
		}
		for _, ip := range ips {
			conn, err := baseDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			dialErr = err
		}
		return nil, dialErr
	}

	transport := &http.Transport{
		DialContext:           guardedDial,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Timeout:   f.timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= f.maxRedirects {
				return fmt.Errorf("%w: %d", ErrTooManyRedirects, len(via))
			}
			if err := f.validateURL(req.URL); err != nil {
				return err
			}
			allowed, crawlDelay, err := f.robotsAllows(req.Context(), req.URL)
			if err != nil {
				return err
			}
			if !allowed {
				return fmt.Errorf("%w: %s", ErrRobotsDisallowed, req.URL.Path)
			}
			f.recordCrawlDelay(req.URL.Hostname(), crawlDelay)
			if err := f.waitHostCrawlDelay(req.Context(), req.URL.Hostname()); err != nil {
				return err
			}
			if err := f.waitHostRateLimit(req.Context(), req.URL); err != nil {
				return err
			}
			return nil
		},
	}
}
