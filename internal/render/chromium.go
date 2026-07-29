package render

import (
	"context"
	"net/url"
	"os/exec"
	"sync"
	"time"

	cdpfetch "github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type chromeRenderer struct {
	browserCtx      context.Context
	cancel          context.CancelFunc
	closeOnce       sync.Once
	resourceFetcher ResourceFetcher
}

func newChromeRenderer(ctx context.Context, resourceFetcher ResourceFetcher, userAgent string) *chromeRenderer {
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("host-resolver-rules", "MAP * 0.0.0.0, EXCLUDE 127.0.0.1"),
		chromedp.UserAgent(userAgent),
	)
	if chromePath, err := exec.LookPath("google-chrome"); err == nil {
		options = append(options, chromedp.ExecPath(chromePath))
	}
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, options...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	return &chromeRenderer{
		browserCtx:      browserCtx,
		resourceFetcher: resourceFetcher,
		cancel: func() {
			cancelBrowser()
			cancelAllocator()
		},
	}
}

func (r *chromeRenderer) Close() {
	r.closeOnce.Do(r.cancel)
}

func (r *chromeRenderer) Render(ctx context.Context, rawURL string, staticBody []byte) ([]byte, error) {
	page, err := newLocalPage(ctx, rawURL, staticBody, r.resourceFetcher)
	if err != nil {
		return nil, err
	}
	defer page.Close()

	tabCtx, cancelTab := chromedp.NewContext(r.browserCtx)
	stop := context.AfterFunc(ctx, cancelTab)
	defer stop()
	defer cancelTab()
	restrictBrowserToPageOrigin(tabCtx, page.URL())

	var html string
	if err := chromedp.Run(tabCtx,
		cdpfetch.Enable(),
		chromedp.Navigate(page.URL().String()),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	); err != nil {
		return nil, err
	}
	return []byte(html), nil
}

func restrictBrowserToPageOrigin(ctx context.Context, allowed *url.URL) {
	chromedp.ListenTarget(ctx, func(event any) {
		paused, ok := event.(*cdpfetch.EventRequestPaused)
		if !ok {
			return
		}
		requestURL, err := url.Parse(paused.Request.URL)
		action := chromedp.Action(cdpfetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient))
		if err == nil && requestURL.Scheme == allowed.Scheme && requestURL.Host == allowed.Host {
			action = cdpfetch.ContinueRequest(paused.RequestID)
		}
		go func() { _ = chromedp.Run(ctx, action) }()
	})
}
