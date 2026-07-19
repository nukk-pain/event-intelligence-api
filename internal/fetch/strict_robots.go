package fetch

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxRobotsBodyBytes = 512 << 10

func readStrictRobots(resp *http.Response, ua string) (*robotsRules, error) {
	limited := io.LimitReader(resp.Body, maxRobotsBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read robots body: %w", err)
	}
	if len(body) > maxRobotsBodyBytes {
		return nil, fmt.Errorf("robots body: %w", ErrBodyTooLarge)
	}
	if len(body) == 0 {
		return &robotsRules{}, nil
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "text/plain") {
		return nil, &DocumentMIMEError{}
	}
	if err := validateStrictRobotsSyntax(body); err != nil {
		return nil, err
	}
	return parseRobots(body, ua), nil
}

func validateStrictRobotsSyntax(body []byte) error {
	if !utf8.Valid(body) {
		return errMalformedRobots
	}
	content := strings.TrimPrefix(string(body), "\ufeff")
	recognized := false
	sawAgent := false
	sawContent := false
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		if line == "" {
			continue
		}
		sawContent = true
		colon := strings.IndexByte(line, ':')
		if colon < 1 {
			return errMalformedRobots
		}
		field := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])
		switch field {
		case "user-agent":
			if value == "" {
				return errMalformedRobots
			}
			recognized = true
			sawAgent = true
		case "allow", "disallow":
			if !sawAgent {
				return errMalformedRobots
			}
		case "crawl-delay":
			if !sawAgent {
				return errMalformedRobots
			}
			seconds, err := strconv.ParseFloat(value, 64)
			if err != nil || seconds < 0 {
				return errMalformedRobots
			}
		case "sitemap":
			if value == "" {
				return errMalformedRobots
			}
			recognized = true
		}
	}
	if sawContent && !recognized {
		return errMalformedRobots
	}
	return nil
}
