package fetch

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

func (f *Fetcher) prepareDocumentBody(raw []byte, contentType, mediaType string) ([]byte, error) {
	if f.strictPublicCrawl && isCompressedPublicDocumentMIME(mediaType) {
		if err := f.accountGzipExpansion(raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	body, err := toUTF8(raw, contentType)
	if err != nil {
		return nil, fmt.Errorf("fetch: charset: %w", err)
	}
	return body, nil
}

func (f *Fetcher) accountGzipExpansion(raw []byte) error {
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("fetch: gzip header: %w", err)
	}
	budgeted := &budgetReader{Reader: reader, budget: f.crawlBudget}
	expanded, err := io.Copy(io.Discard, io.LimitReader(budgeted, f.maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("fetch: gzip expansion: %w", err)
	}
	if expanded > f.maxBodyBytes {
		return fmt.Errorf("%w: decompressed > %d", ErrBodyTooLarge, f.maxBodyBytes)
	}
	return nil
}
