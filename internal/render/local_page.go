package render

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

type localPage struct {
	context         context.Context
	sourceURL       *url.URL
	localURL        *url.URL
	staticBody      []byte
	resourceFetcher ResourceFetcher
	server          *http.Server
	listener        net.Listener
	closeOnce       sync.Once
}

func newLocalPage(ctx context.Context, rawURL string, staticBody []byte, resourceFetcher ResourceFetcher) (*localPage, error) {
	sourceURL, err := url.Parse(rawURL)
	if err != nil || (sourceURL.Scheme != "http" && sourceURL.Scheme != "https") || sourceURL.Hostname() == "" {
		return nil, fmt.Errorf("invalid render URL %q", rawURL)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen local renderer: %w", err)
	}
	localURL, err := url.Parse("http://" + listener.Addr().String() + sourceURL.RequestURI())
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("build local renderer URL: %w", err)
	}
	localURL.Fragment = sourceURL.Fragment
	page := &localPage{
		context:         ctx,
		sourceURL:       sourceURL,
		localURL:        localURL,
		resourceFetcher: resourceFetcher,
		listener:        listener,
	}
	page.staticBody = rewriteForLocalOrigin(staticBody, sourceURL, localURL)
	page.server = &http.Server{Handler: page}
	go func() { _ = page.server.Serve(listener) }()
	return page, nil
}

func (p *localPage) URL() *url.URL {
	copy := *p.localURL
	return &copy
}

func (p *localPage) Close() {
	p.closeOnce.Do(func() { _ = p.server.Close() })
}

func (p *localPage) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		http.Error(w, "renderer accepts GET only", http.StatusMethodNotAllowed)
		return
	}
	upstream := p.sourceURL.ResolveReference(request.URL)
	if !sameOrigin(upstream, p.sourceURL) {
		http.Error(w, "cross-origin renderer request", http.StatusForbidden)
		return
	}
	if sameRequest(upstream, p.sourceURL) {
		p.writeResource(w, Resource{Body: p.staticBody, ContentType: "text/html; charset=utf-8"})
		return
	}
	resource, err := p.resourceFetcher.Fetch(p.context, upstream.String())
	if err != nil {
		http.Error(w, "renderer resource unavailable", http.StatusBadGateway)
		return
	}
	p.writeResource(w, resource)
}

func (p *localPage) writeResource(w http.ResponseWriter, resource Resource) {
	if resource.ContentType != "" {
		w.Header().Set("Content-Type", resource.ContentType)
	}
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(resource.Body)
}

func rewriteForLocalOrigin(body []byte, sourceURL, localURL *url.URL) []byte {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return body
	}
	document.Find("base").Remove()
	document.Find("script[src], iframe[src], link[href]").Each(func(_ int, selection *goquery.Selection) {
		attribute := "src"
		if selection.Is("link") {
			attribute = "href"
		}
		value, ok := selection.Attr(attribute)
		if !ok {
			return
		}
		rewritten, local := rewriteURL(value, sourceURL, localURL)
		if !local {
			selection.Remove()
			return
		}
		selection.SetAttr(attribute, rewritten)
	})
	document.Find("[src], [href]").Each(func(_ int, selection *goquery.Selection) {
		for _, attribute := range []string{"src", "href"} {
			if attribute == "href" && selection.Is("a") {
				continue
			}
			value, ok := selection.Attr(attribute)
			if !ok {
				continue
			}
			if rewritten, local := rewriteURL(value, sourceURL, localURL); local {
				selection.SetAttr(attribute, rewritten)
			} else if absoluteURL(value) {
				selection.RemoveAttr(attribute)
			}
		}
	})
	sourceOrigin := sourceURL.Scheme + "://" + sourceURL.Host
	localOrigin := localURL.Scheme + "://" + localURL.Host
	document.Find("script:not([src])").Each(func(_ int, selection *goquery.Selection) {
		selection.SetHtml(strings.ReplaceAll(selection.Text(), sourceOrigin, localOrigin))
	})
	html, err := document.Html()
	if err != nil {
		return body
	}
	return []byte(html)
}

func rewriteURL(rawURL string, sourceURL, localURL *url.URL) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "data" || parsed.Scheme == "mailto" || parsed.Scheme == "javascript" {
		return rawURL, true
	}
	if parsed.IsAbs() && sameOrigin(parsed, localURL) {
		return rawURL, true
	}
	resolved := sourceURL.ResolveReference(parsed)
	if !sameOrigin(resolved, sourceURL) {
		return rawURL, false
	}
	if !parsed.IsAbs() && parsed.Host == "" {
		return rawURL, true
	}
	parsed.Scheme = localURL.Scheme
	parsed.Host = localURL.Host
	return parsed.String(), true
}

func absoluteURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && (parsed.IsAbs() || parsed.Host != "")
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func sameRequest(left, right *url.URL) bool {
	return sameOrigin(left, right) && left.EscapedPath() == right.EscapedPath() && left.RawQuery == right.RawQuery
}
