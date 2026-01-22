package websearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

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
			for k, v := range req.Header {
				newReq.Header[k] = v
			}
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
			lastErr = fmt.Errorf("server returned %d", resp.StatusCode)
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

func SearchDuckDuckGo(ctx context.Context, query string, maxResults int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("empty query")
	}
	if maxResults <= 0 {
		maxResults = 8
	}

	searchURL := "https://duckduckgo.com/html/?q=" + url.QueryEscape(query)
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
		return nil, fmt.Errorf("search failed: server returned %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, maxResults)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			class := attr(n, "class")
			if strings.Contains(class, "result__a") || strings.Contains(class, "result-link") {
				title := strings.TrimSpace(textContent(n))
				href := normalizeURL(attr(n, "href"))
				if title != "" && href != "" {
					snippet := findSnippet(n)
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

	return results, nil
}

func FetchText(ctx context.Context, pageURL string, maxChars int, useReaderProxy bool, proxyURLs []string) ([]string, string, string, error) {
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		return nil, "html", "", errors.New("empty url")
	}
	if maxChars <= 0 {
		maxChars = 8000
	}

	var proxyErr string

	// Try reader proxy first if enabled - it handles JS-rendered pages better
	if useReaderProxy {
		lines, err := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
		if err == nil && len(lines) > 0 && !isEmptyReaderLines(lines) {
			return lines, "proxy", "", nil
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
			// Update user agent on redirect
			req.Header.Set("User-Agent", getRandomUserAgent())
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "html", proxyErr, err
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
			lines, proxyErr2 := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
			if proxyErr2 == nil && len(lines) > 0 && !isEmptyReaderLines(lines) {
				return lines, "proxy", "", nil
			}
		}
		return nil, "html", proxyErr, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try proxy as fallback on HTTP errors
		if !useReaderProxy {
			lines, _ := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
			if len(lines) > 0 && !isEmptyReaderLines(lines) {
				return lines, "proxy", "", nil
			}
		}
		return nil, "html", proxyErr, fmt.Errorf("preview failed: server returned %d", resp.StatusCode)
	}

	limitReader := io.LimitReader(resp.Body, int64(maxChars*20))
	contentType := resp.Header.Get("Content-Type")

	// Handle plain text
	if strings.Contains(contentType, "text/plain") {
		body, err := io.ReadAll(limitReader)
		if err != nil {
			return nil, "html", proxyErr, err
		}
		return splitLines(trimText(string(body), maxChars)), "html", proxyErr, nil
	}

	// Handle JSON (API responses)
	if strings.Contains(contentType, "application/json") {
		body, err := io.ReadAll(limitReader)
		if err != nil {
			return nil, "html", proxyErr, err
		}
		// Pretty format JSON for readability
		text := string(body)
		text = strings.ReplaceAll(text, ",", ",\n")
		text = strings.ReplaceAll(text, "{", "{\n")
		text = strings.ReplaceAll(text, "}", "\n}")
		return splitLines(trimText(text, maxChars)), "json", proxyErr, nil
	}

	doc, err := html.Parse(limitReader)
	if err != nil {
		return nil, "html", proxyErr, err
	}

	// Try to find main content first (article, main tags)
	text := extractMainContent(doc, maxChars)
	if strings.TrimSpace(text) == "" {
		// Fall back to full text extraction
		text = extractText(doc, maxChars)
	}

	if strings.TrimSpace(text) == "" {
		// Try proxy as last resort for JS-rendered pages
		proxyLines, proxyErr2 := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
		if proxyErr2 == nil && len(proxyLines) > 0 && !isEmptyReaderLines(proxyLines) {
			return proxyLines, "proxy", "", nil
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
		return fallbackLines, "html", proxyErr, nil
	}
	return splitLines(text), "html", proxyErr, nil
}

// extractMainContent tries to find and extract content from main/article elements
func extractMainContent(doc *html.Node, maxChars int) string {
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
			case "p", "div", "section", "li", "ul", "ol", "h1", "h2", "h3", "h4", "h5", "h6", "table", "tr", "blockquote":
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

func extractText(doc *html.Node, maxChars int) string {
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
			// Block elements get newlines
			case "p", "div", "section", "article", "li", "ul", "ol",
				"h1", "h2", "h3", "h4", "h5", "h6", "table", "tr", "blockquote":
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
		text = text[:maxChars] + "..."
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
	if strings.HasPrefix(href, "//") {
		return "https:" + href
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

func fetchViaReaderProxy(ctx context.Context, pageURL string, maxChars int, proxyURLs []string) ([]string, error) {
	normalizedURL := normalizeReaderURL(pageURL)
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
			return rawLines, nil
		}
		return cleaned, nil
	}

	if lastErr == nil {
		lastErr = errors.New("reader proxy failed")
	}
	return nil, lastErr
}

func normalizeReaderURL(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err == nil && u.Scheme == "" {
		u.Scheme = "https"
		pageURL = u.String()
	}
	if strings.HasPrefix(pageURL, "https://") {
		return "https://" + strings.TrimPrefix(pageURL, "https://")
	}
	if strings.HasPrefix(pageURL, "http://") {
		return "http://" + strings.TrimPrefix(pageURL, "http://")
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
