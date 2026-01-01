package websearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

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
	req.Header.Set("User-Agent", "RavenTerminal/1.0")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("search failed")
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
	if useReaderProxy {
		lines, err := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
		if err == nil && len(lines) > 0 && !isEmptyReaderLines(lines) {
			return lines, "proxy", "", nil
		}
		if err != nil {
			proxyErr = err.Error()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "html", proxyErr, err
	}
	req.Header.Set("User-Agent", "RavenTerminal/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "html", proxyErr, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "html", proxyErr, errors.New("preview failed")
	}

	limitReader := io.LimitReader(resp.Body, int64(maxChars*20))
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/plain") {
		body, err := io.ReadAll(limitReader)
		if err != nil {
			return nil, "html", proxyErr, err
		}
		return splitLines(trimText(string(body), maxChars)), "html", proxyErr, nil
	}

	doc, err := html.Parse(limitReader)
	if err != nil {
		return nil, "html", proxyErr, err
	}

	text := extractText(doc, maxChars)
	if strings.TrimSpace(text) == "" {
		if useReaderProxy {
			lines, err := fetchViaReaderProxy(ctx, pageURL, maxChars, proxyURLs)
			if err == nil && len(lines) > 0 && !isEmptyReaderLines(lines) {
				return lines, "proxy", "", nil
			}
			if err != nil {
				proxyErr = err.Error()
			}
		}
		title, desc := extractMeta(doc)
		lines := []string{
			"(no readable text found; page may be JS-rendered)",
		}
		if title != "" {
			lines = append(lines, "Title: "+title)
		}
		if desc != "" {
			lines = append(lines, "Description: "+desc)
		}
		return lines, "html", proxyErr, nil
	}
	return splitLines(text), "html", proxyErr, nil
}

func extractText(doc *html.Node, maxChars int) string {
	var sb strings.Builder
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inPre bool) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "svg", "img", "video", "audio", "canvas":
				return
			case "br":
				sb.WriteString("\n")
			case "p", "div", "section", "article", "header", "footer", "li", "ul", "ol", "pre", "code",
				"h1", "h2", "h3", "h4", "h5", "h6", "table", "tr":
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
			nextInPre := inPre || (n.Type == html.ElementNode && (n.Data == "pre" || n.Data == "code"))
			walk(c, nextInPre)
			if sb.Len() >= maxChars {
				return
			}
		}
	}
	walk(doc, false)

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
		proxies = []string{"https://r.jina.ai/"}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error

	for _, base := range proxies {
		readerURL := buildProxyURL(base, normalizedURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, readerURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request for %s: %w", readerURL, err)
			continue
		}
		req.Header.Set("User-Agent", "RavenTerminal/1.0")
		req.Header.Set("Accept", "text/plain")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
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
