package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// ErrNoResults means both search endpoints returned pages we could not pull
// results out of — usually a markup change, not a genuinely empty query.
var ErrNoResults = errors.New("no results (page layout may have changed)")

// StatusError reports a non-success HTTP status so callers can tell rate
// limiting apart from other failures.
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("server returned %d", e.Code)
}

// ErrorReason condenses a search/fetch error into a short cause suitable for
// a one-line status display.
func ErrorReason(err error) string {
	if err == nil {
		return ""
	}
	var statusErr *StatusError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &statusErr):
		if statusErr.Code == http.StatusTooManyRequests {
			return "rate limited (429)"
		}
		return fmt.Sprintf("HTTP %d", statusErr.Code)
	}
	return err.Error()
}

// ValidatePublicHTTPURL rejects targets that could expose services on the
// local machine or private network. Ollama has its own explicitly configured
// client; arbitrary page fetching is intentionally public-web only.
func ValidatePublicHTTPURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("only http(s) URLs are supported")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return errors.New("url has no host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("local and private network URLs are not allowed")
	}

	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		resolved, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return fmt.Errorf("resolve host: %w", err)
		}
		ips = resolved
	}
	if len(ips) == 0 {
		return errors.New("host resolved to no addresses")
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return errors.New("local and private network URLs are not allowed")
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Carrier-grade NAT is not covered by net.IP.IsPrivate.
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return !cgnat.Contains(ip)
}

// userAgents is a list of common user agents to rotate through
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
}

// getRandomUserAgent returns a random user agent string
func getRandomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}

// retryConfig holds retry settings
type retryConfig struct {
	maxRetries int
	baseDelay  time.Duration
}

var defaultRetryConfig = retryConfig{
	maxRetries: 3,
	baseDelay:  time.Second,
}

// doWithRetry executes an HTTP request with exponential backoff retry
func doWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < defaultRetryConfig.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s
			delay := defaultRetryConfig.baseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			// Clone request for retry with new user agent
			newReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL.String(), nil)
			if err != nil {
				return nil, err
			}
			maps.Copy(newReq.Header, req.Header)
			newReq.Header.Set("User-Agent", getRandomUserAgent())
			req = newReq
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// Retry on server errors (5xx) or rate limiting (429)
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			resp.Body.Close()
			lastErr = &StatusError{Code: resp.StatusCode}
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("request failed after %d retries: %w", defaultRetryConfig.maxRetries, lastErr)
	}
	return nil, errors.New("request failed: unknown error")
}

type Result struct {
	Title   string
	URL     string
	Snippet string
}

// Link is a followable link extracted from a fetched page. Link i (0-based)
// carries the inline "[i+1]" marker appended after its text in the extracted
// lines. Occ disambiguates that marker from identical literal page text (e.g.
// a Wikipedia citation "[3]"): it counts how many "[i+1]" occurrences appear
// in the extracted text before the marker itself.
type Link struct {
	Text string
	URL  string
	Occ  int
}

// maxLinks caps how many links are extracted (and numbered) per page.
const maxLinks = 99

// resolveLink resolves an anchor href against the page URL, returning "" for
// anything that is not a followable http(s) link (fragments, javascript:,
// mailto:, malformed URLs).
func resolveLink(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

// DirectURL reports whether query looks like a URL rather than search terms
// (explicit http(s) scheme, or a dotted host with no spaces whose last label
// looks like a TLD) and returns it normalized with a scheme.
func DirectURL(query string) (string, bool) {
	q := strings.TrimSpace(query)
	if q == "" || strings.ContainsAny(q, " \t") {
		return "", false
	}
	if strings.HasPrefix(q, "http://") || strings.HasPrefix(q, "https://") {
		if u, err := url.Parse(q); err == nil && u.Host != "" {
			return q, true
		}
		return "", false
	}
	if !strings.Contains(q, ".") {
		return "", false
	}
	u, err := url.Parse("https://" + q)
	if err != nil || u.Host == "" || !strings.Contains(u.Host, ".") {
		return "", false
	}
	// Schemeless matching is a heuristic: require a letters-only last label
	// (a plausible TLD) or an IP literal so dotted search terms like "3.14"
	// or "go1.22.1" stay searches.
	if net.ParseIP(u.Hostname()) == nil && !plausibleTLD(u.Hostname()) {
		return "", false
	}
	return "https://" + q, true
}

// plausibleTLD reports whether host's last dot label looks like a TLD
// (letters only, at least two of them).
func plausibleTLD(host string) bool {
	last := host[strings.LastIndexByte(host, '.')+1:]
	if len(last) < 2 {
		return false
	}
	for _, r := range last {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// parseMarkdownLinkLine rewrites markdown [text](url) links in one line to
// "text[n]" markers, appending each followable target to links. Image links
// ("![alt](src)") are reduced to their alt text without a marker. prior is
// the already-emitted text of earlier lines, used to count "[n]" occurrences
// so markers stay distinguishable from identical literal text.
func parseMarkdownLinkLine(line string, base *url.URL, links *[]Link, prior string) string {
	var sb strings.Builder
	for i := 0; i < len(line); {
		start := strings.Index(line[i:], "[")
		if start < 0 {
			sb.WriteString(line[i:])
			break
		}
		start += i
		mid := strings.Index(line[start:], "](")
		if mid < 0 {
			sb.WriteString(line[i:])
			break
		}
		mid += start
		end := strings.Index(line[mid+2:], ")")
		if end < 0 {
			sb.WriteString(line[i:])
			break
		}
		end += mid + 2
		text := line[start+1 : mid]
		target := line[mid+2 : end]
		// Drop a markdown title suffix: (url "title").
		if sp := strings.IndexByte(target, ' '); sp >= 0 {
			target = target[:sp]
		}
		isImage := start > 0 && line[start-1] == '!'
		if isImage {
			sb.WriteString(line[i : start-1])
		} else {
			sb.WriteString(line[i:start])
		}
		resolved := ""
		if !isImage && len(*links) < maxLinks {
			resolved = resolveLink(base, target)
		}
		sb.WriteString(text)
		if resolved != "" {
			marker := fmt.Sprintf("[%d]", len(*links)+1)
			*links = append(*links, Link{Text: collapseSpace(text), URL: resolved,
				Occ: strings.Count(prior, marker) + strings.Count(sb.String(), marker)})
			sb.WriteString(marker)
		}
		i = end + 1
	}
	return sb.String()
}

// extractMarkdownLinks runs parseMarkdownLinkLine over reader-proxy lines.
func extractMarkdownLinks(lines []string, base *url.URL) ([]string, []Link) {
	var links []Link
	var prior strings.Builder // emitted text so far, for marker occurrence counts
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = parseMarkdownLinkLine(line, base, &links, prior.String())
		prior.WriteString(out[i])
		prior.WriteByte('\n')
	}
	return out, links
}

func SearchDuckDuckGo(ctx context.Context, query string, maxResults int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("empty query")
	}
	if maxResults <= 0 {
		maxResults = 8
	}

	doc, err := fetchSearchPage(ctx, "https://duckduckgo.com/html/?q="+url.QueryEscape(query))
	if err == nil {
		if results := parseHTMLResults(doc, maxResults); len(results) > 0 {
			return results, nil
		}
	}

	// Fall back to the lite endpoint when the html one fails or its markup
	// changed under us and parsed to nothing.
	liteDoc, liteErr := fetchSearchPage(ctx, "https://lite.duckduckgo.com/lite/?q="+url.QueryEscape(query))
	if liteErr == nil {
		if results := parseLiteResults(liteDoc, maxResults); len(results) > 0 {
			return results, nil
		}
	}

	if err != nil {
		return nil, err
	}
	if liteErr != nil {
		return nil, liteErr
	}
	return nil, ErrNoResults
}

// fetchSearchPage fetches and parses one search endpoint page.
func fetchSearchPage(ctx context.Context, searchURL string) (*html.Node, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", getRandomUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := doWithRetry(ctx, client, req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search failed: %w", &StatusError{Code: resp.StatusCode})
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("search parse failed: %w", err)
	}
	return doc, nil
}

// parseHTMLResults extracts results from the duckduckgo.com/html markup
// (anchors classed result__a / result-link).
func parseHTMLResults(doc *html.Node, maxResults int) []Result {
	results := make([]Result, 0, maxResults)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			class := attr(n, "class")
			if strings.Contains(class, "result__a") || strings.Contains(class, "result-link") {
				title := collapseSpace(textContent(n))
				href := normalizeURL(attr(n, "href"))
				if title != "" && href != "" {
					snippet := collapseSpace(findSnippet(n))
					results = append(results, Result{
						Title:   title,
						URL:     href,
						Snippet: snippet,
					})
					if len(results) >= maxResults {
						return
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if len(results) >= maxResults {
				return
			}
		}
	}
	walk(doc)
	return results
}

// parseLiteResults extracts results from the lite.duckduckgo.com table
// markup: result anchors carry class "result-link" and each is followed (in
// document order) by a cell classed "result-snippet". Tolerant of wrapper
// changes — it only keys off those class names.
func parseLiteResults(doc *html.Node, maxResults int) []Result {
	var results []Result
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			class := attr(n, "class")
			if n.Data == "a" && strings.Contains(class, "result-link") {
				title := collapseSpace(textContent(n))
				href := normalizeURL(attr(n, "href"))
				if title != "" && href != "" && len(results) < maxResults {
					results = append(results, Result{Title: title, URL: href})
				}
				return
			}
			if strings.Contains(class, "result-snippet") {
				if len(results) > 0 && results[len(results)-1].Snippet == "" {
					results[len(results)-1].Snippet = collapseSpace(textContent(n))
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

func FetchText(ctx context.Context, pageURL string, maxChars int, useReaderProxy bool, proxyURLs []string) ([]string, []Link, string, string, error) {
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		return nil, nil, "html", "", errors.New("empty url")
	}
	if err := ValidatePublicHTTPURL(ctx, pageURL); err != nil {
		return nil, nil, "html", "", err
	}
	if maxChars <= 0 {
		maxChars = 8000
	}

	var proxyErr string

	// Try reader proxy first if enabled - it handles JS-rendered pages better
	if useReaderProxy {
		lines, links, err := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
		if err == nil && len(lines) > 0 && !isEmptyReaderLines(lines) {
			return lines, links, "proxy", "", nil
		}
		if err != nil {
			proxyErr = err.Error()
		}
	}

	// Create client that follows redirects
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if err := ValidatePublicHTTPURL(req.Context(), req.URL.String()); err != nil {
				return err
			}
			// Update user agent on redirect
			req.Header.Set("User-Agent", getRandomUserAgent())
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, nil, "html", proxyErr, err
	}
	req.Header.Set("User-Agent", getRandomUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "identity") // Avoid compression issues
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := doWithRetry(ctx, client, req)
	if err != nil {
		// If direct fetch fails and we haven't tried proxy yet, try it now
		if !useReaderProxy {
			lines, links, proxyErr2 := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
			if proxyErr2 == nil && len(lines) > 0 && !isEmptyReaderLines(lines) {
				return lines, links, "proxy", "", nil
			}
		}
		return nil, nil, "html", proxyErr, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try proxy as fallback on HTTP errors
		if !useReaderProxy {
			lines, links, _ := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
			if len(lines) > 0 && !isEmptyReaderLines(lines) {
				return lines, links, "proxy", "", nil
			}
		}
		return nil, nil, "html", proxyErr, fmt.Errorf("preview failed: %w", &StatusError{Code: resp.StatusCode})
	}

	limitReader := io.LimitReader(resp.Body, int64(maxChars*20))
	contentType := resp.Header.Get("Content-Type")
	// Decode non-UTF-8 pages (charset sniffs the Content-Type and the body).
	reader, err := charset.NewReader(limitReader, contentType)
	if err != nil {
		reader = limitReader
	}

	// Handle plain text
	if strings.Contains(contentType, "text/plain") {
		body, err := io.ReadAll(reader)
		if err != nil {
			return nil, nil, "html", proxyErr, err
		}
		return splitLines(trimText(string(body), maxChars)), nil, "html", proxyErr, nil
	}

	// Handle JSON (API responses)
	if strings.Contains(contentType, "application/json") {
		body, err := io.ReadAll(reader)
		if err != nil {
			return nil, nil, "html", proxyErr, err
		}
		// Pretty format JSON for readability; fall back to raw text if invalid
		text := string(body)
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err == nil {
			text = pretty.String()
		}
		return splitLines(trimText(text, maxChars)), nil, "json", proxyErr, nil
	}

	doc, err := html.Parse(reader)
	if err != nil {
		return nil, nil, "html", proxyErr, fmt.Errorf("preview parse failed: %w", err)
	}

	// Resolve relative anchor hrefs against the final (post-redirect) URL.
	base := resp.Request.URL

	// Try to find main content first (article, main tags)
	var links []Link
	text := extractMainContent(doc, maxChars, base, &links)
	if strings.TrimSpace(text) == "" {
		// Fall back to full text extraction
		links = nil
		text = extractText(doc, maxChars, base, &links)
	}

	if strings.TrimSpace(text) == "" {
		// Try proxy as last resort for JS-rendered pages
		proxyLines, proxyLinks, proxyErr2 := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
		if proxyErr2 == nil && len(proxyLines) > 0 && !isEmptyReaderLines(proxyLines) {
			return proxyLines, proxyLinks, "proxy", "", nil
		}
		if proxyErr2 != nil && proxyErr == "" {
			proxyErr = proxyErr2.Error()
		}

		title, desc := extractMeta(doc)
		fallbackLines := []string{
			"(no readable text found; page may be JS-rendered)",
		}
		if title != "" {
			fallbackLines = append(fallbackLines, "Title: "+title)
		}
		if desc != "" {
			fallbackLines = append(fallbackLines, "Description: "+desc)
		}
		return fallbackLines, nil, "html", proxyErr, nil
	}
	return splitLines(text), links, "html", proxyErr, nil
}

// extractMainContent tries to find and extract content from main/article elements
func extractMainContent(doc *html.Node, maxChars int, base *url.URL, links *[]Link) string {
	// Look for article or main content areas
	var mainNode *html.Node
	var findMain func(*html.Node)
	findMain = func(n *html.Node) {
		if mainNode != nil {
			return
		}
		if n.Type == html.ElementNode {
			// Priority: article > main > [role="main"] > .content/.post/.entry
			if n.Data == "article" || n.Data == "main" {
				mainNode = n
				return
			}
			// Check for role="main" or common content classes
			for _, a := range n.Attr {
				if a.Key == "role" && a.Val == "main" {
					mainNode = n
					return
				}
				if a.Key == "class" || a.Key == "id" {
					lower := strings.ToLower(a.Val)
					if strings.Contains(lower, "article") ||
						strings.Contains(lower, "post-content") ||
						strings.Contains(lower, "entry-content") ||
						strings.Contains(lower, "main-content") {
						mainNode = n
						return
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findMain(c)
		}
	}
	findMain(doc)

	if mainNode == nil {
		return ""
	}

	// Extract text from the main content node only
	var sb strings.Builder
	var walk func(*html.Node, bool, int)
	walk = func(n *html.Node, inPre bool, depth int) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "svg", "img", "video", "audio", "canvas", "iframe", "nav", "aside":
				return
			case "footer", "header":
				if depth < 2 {
					return
				}
			case "br":
				sb.WriteString("\n")
			case "pre":
				sb.WriteString("\n```\n")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, true, depth+1)
				}
				sb.WriteString("\n```\n")
				return
			case "code":
				if !inPre {
					sb.WriteString("`")
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c, true, depth+1)
					}
					sb.WriteString("`")
					return
				}
			// Followable links get an inline "[n]" marker after their text.
			case "a":
				if links != nil && len(*links) < maxLinks {
					if target := resolveLink(base, attr(n, "href")); target != "" {
						for c := n.FirstChild; c != nil; c = c.NextSibling {
							walk(c, inPre, depth+1)
						}
						marker := fmt.Sprintf("[%d]", len(*links)+1)
						*links = append(*links, Link{Text: collapseSpace(textContent(n)), URL: target, Occ: strings.Count(sb.String(), marker)})
						sb.WriteString(marker + " ")
						return
					}
				}
			// Structure markers: the preview renderer highlights headings and
			// bullets keyed off these prefixes.
			case "h1", "h2", "h3", "h4", "h5", "h6":
				sb.WriteString("\n## ")
			case "li":
				sb.WriteString("\n• ")
			case "p", "div", "section", "ul", "ol", "table", "tr", "blockquote":
				sb.WriteString("\n")
			}
		}

		if n.Type == html.TextNode {
			text := n.Data
			if !inPre {
				text = strings.TrimSpace(text)
			}
			if text != "" {
				sb.WriteString(text)
				if !strings.HasSuffix(text, "\n") {
					sb.WriteString(" ")
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inPre, depth+1)
			if sb.Len() >= maxChars {
				return
			}
		}
	}
	walk(mainNode, false, 0)

	return trimText(sb.String(), maxChars)
}

func extractText(doc *html.Node, maxChars int, base *url.URL, links *[]Link) string {
	var sb strings.Builder
	var walk func(*html.Node, bool, int)
	walk = func(n *html.Node, inPre bool, depth int) {
		if n.Type == html.ElementNode {
			switch n.Data {
			// Skip elements that don't contain useful content
			case "script", "style", "noscript", "svg", "img", "video", "audio", "canvas", "iframe":
				return
			// Skip navigation and boilerplate elements (but only at top levels)
			case "nav", "aside":
				return
			// Skip footer if it looks like site footer (not article footer)
			case "footer":
				if depth < 4 {
					return
				}
			// Skip header if it looks like site header (not article header)
			case "header":
				if depth < 3 {
					return
				}
			case "br":
				sb.WriteString("\n")
			// Code blocks get special markers
			case "pre":
				sb.WriteString("\n```\n")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, true, depth+1)
				}
				sb.WriteString("\n```\n")
				return
			case "code":
				if !inPre {
					// Inline code
					sb.WriteString("`")
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c, true, depth+1)
					}
					sb.WriteString("`")
					return
				}
			// Followable links get an inline "[n]" marker after their text.
			case "a":
				if links != nil && len(*links) < maxLinks {
					if target := resolveLink(base, attr(n, "href")); target != "" {
						for c := n.FirstChild; c != nil; c = c.NextSibling {
							walk(c, inPre, depth+1)
						}
						marker := fmt.Sprintf("[%d]", len(*links)+1)
						*links = append(*links, Link{Text: collapseSpace(textContent(n)), URL: target, Occ: strings.Count(sb.String(), marker)})
						sb.WriteString(marker + " ")
						return
					}
				}
			// Structure markers: the preview renderer highlights headings and
			// bullets keyed off these prefixes.
			case "h1", "h2", "h3", "h4", "h5", "h6":
				sb.WriteString("\n## ")
			case "li":
				sb.WriteString("\n• ")
			// Block elements get newlines
			case "p", "div", "section", "article", "ul", "ol",
				"table", "tr", "blockquote":
				sb.WriteString("\n")
			}
		}

		if n.Type == html.TextNode {
			text := n.Data
			if !inPre {
				text = strings.TrimSpace(text)
			}
			if text != "" {
				sb.WriteString(text)
				if !strings.HasSuffix(text, "\n") {
					sb.WriteString(" ")
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inPre, depth+1)
			if sb.Len() >= maxChars {
				return
			}
		}
	}
	walk(doc, false, 0)

	return trimText(sb.String(), maxChars)
}

func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{"(no readable text found)"}
	}
	return out
}

func trimText(text string, maxChars int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\t", " ")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.Join(strings.Fields(line), " ")
		lines[i] = line
	}
	text = strings.Join(lines, "\n")
	if len(text) > maxChars {
		cut := maxChars
		// Never cut in the middle of a multi-byte rune.
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "..."
	}
	return text
}

func findSnippet(anchor *html.Node) string {
	result := findAncestor(anchor, func(n *html.Node) bool {
		class := attr(n, "class")
		return strings.Contains(class, "result") || strings.Contains(class, "results")
	})
	if result == nil {
		return ""
	}
	var snippet string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			class := attr(n, "class")
			if strings.Contains(class, "result__snippet") || strings.Contains(class, "result-snippet") || strings.Contains(class, "snippet") {
				snippet = strings.TrimSpace(textContent(n))
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if snippet != "" {
				return
			}
		}
	}
	walk(result)
	return snippet
}

func findAncestor(n *html.Node, fn func(*html.Node) bool) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if fn(p) {
			return p
		}
	}
	return nil
}

func normalizeURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	// Protocol-relative links (the form DuckDuckGo's redirect links usually
	// take) must still go through the redirect-decoding below.
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	if strings.HasPrefix(href, "/l/?") {
		u, err := url.Parse("https://duckduckgo.com" + href)
		if err == nil {
			if target := u.Query().Get("uddg"); target != "" {
				return target
			}
		}
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		u, err := url.Parse(href)
		if err == nil && strings.Contains(u.Host, "duckduckgo.com") && strings.HasPrefix(u.Path, "/l/") {
			if target := u.Query().Get("uddg"); target != "" {
				return target
			}
		}
	}
	return href
}

// collapseSpace trims and collapses all interior whitespace runs (newlines,
// tabs, repeated spaces from HTML) to single spaces so results render as one
// clean line.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func fetchViaReaderProxy(ctx context.Context, pageURL string, maxChars int, proxyURLs []string) ([]string, []Link, error) {
	normalizedURL := normalizeReaderURL(pageURL)
	linkBase, _ := url.Parse(normalizedURL)
	proxies := proxyURLs
	if len(proxies) == 0 {
		// Default reader proxies - jina.ai is most reliable
		proxies = []string{
			"https://r.jina.ai/",
			"https://web.scraper.workers.dev/?url={url}&selector=body",
		}
	}

	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error

	for _, base := range proxies {
		readerURL := buildProxyURL(base, normalizedURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, readerURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request for %s: %w", readerURL, err)
			continue
		}
		req.Header.Set("User-Agent", getRandomUserAgent())
		req.Header.Set("Accept", "text/plain,text/html;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.5")

		resp, err := doWithRetry(ctx, client, req)
		if err != nil {
			lastErr = fmt.Errorf("proxy request failed: %w", err)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("proxy failed for %s: %s", readerURL, resp.Status)
			resp.Body.Close()
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxChars*2)))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		raw := string(body)
		raw = strings.ReplaceAll(raw, "\r\n", "\n")
		raw = strings.ReplaceAll(raw, "\r", "\n")
		rawLines := splitLines(raw)
		cleaned := cleanReaderLines(rawLines)
		if len(cleaned) == 0 {
			cleaned = rawLines
		}
		// Reader output is markdown: rewrite [text](url) links to text[n]
		// markers and collect the targets.
		lines, links := extractMarkdownLinks(cleaned, linkBase)
		return lines, links, nil
	}

	if lastErr == nil {
		lastErr = errors.New("reader proxy failed")
	}
	return nil, nil, lastErr
}

func normalizeReaderURL(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err == nil && u.Scheme == "" {
		u.Scheme = "https"
		pageURL = u.String()
	}
	if after, ok := strings.CutPrefix(pageURL, "https://"); ok {
		return "https://" + after
	}
	if after, ok := strings.CutPrefix(pageURL, "http://"); ok {
		return "http://" + after
	}
	return "http://" + pageURL
}

func buildProxyURL(base, target string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return target
	}
	if strings.Contains(base, "{url}") {
		return strings.ReplaceAll(base, "{url}", target)
	}
	if strings.HasSuffix(base, "/http://") {
		return base + stripScheme(target)
	}
	if strings.HasSuffix(base, "/https://") {
		return base + stripScheme(target)
	}
	if strings.HasSuffix(base, "/") {
		return base + target
	}
	return base + "/" + target
}

func stripScheme(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "https://")
	target = strings.TrimPrefix(target, "http://")
	return target
}

func cleanReaderLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(lower, "url source:"):
			continue
		case strings.HasPrefix(lower, "markdown content"):
			continue
		case strings.HasPrefix(lower, "title:"):
			continue
		case strings.HasPrefix(lower, "content-type:"):
			continue
		case strings.HasPrefix(trimmed, "![") || strings.HasPrefix(trimmed, "[!"):
			continue
		case trimmed == "---":
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func isEmptyReaderLines(lines []string) bool {
	if len(lines) == 0 {
		return true
	}
	if len(lines) == 1 {
		return strings.Contains(strings.ToLower(lines[0]), "no readable text")
	}
	return false
}

func extractMeta(doc *html.Node) (string, string) {
	var title string
	var desc string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "title" && title == "" {
				title = strings.TrimSpace(textContent(n))
			}
			if n.Data == "meta" && desc == "" {
				var name, content string
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "name", "property":
						name = strings.ToLower(a.Val)
					case "content":
						content = strings.TrimSpace(a.Val)
					}
				}
				if (name == "description" || name == "og:description") && content != "" {
					desc = content
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return title, desc
}
