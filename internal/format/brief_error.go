// brief_error.go shortens arbitrary errors for terminal tables and stderr so
// HTML-heavy CDN responses never fill the screen.
package format

import (
	"strings"
)

const briefErrorMaxRunes = 240

// BriefError returns a short, single-line summary of err for tables and stderr.
// It collapses whitespace, detects HTML/CDN error pages (and prefers <title>),
// and caps length so RPC failures never flood the terminal.
func BriefError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	s = strings.TrimPrefix(s, "\ufeff")
	return briefString(s)
}

func briefString(s string) string {
	oneLine := strings.Join(strings.Fields(s), " ")
	lower := strings.ToLower(oneLine)

	if strings.Contains(lower, "<html") ||
		strings.HasPrefix(lower, "<!doctype html") ||
		(strings.Contains(lower, "<title") && strings.Contains(lower, "</title>")) {
		if title, ok := extractHTMLTitleBrief(s); ok {
			out := "HTML error page: " + strings.TrimSpace(title)
			return truncateBrief(out, briefErrorMaxRunes)
		}
		return "HTML error page (CDN or proxy; body omitted)"
	}

	return truncateBrief(oneLine, briefErrorMaxRunes)
}

func extractHTMLTitleBrief(html string) (string, bool) {
	lower := strings.ToLower(html)
	idx := strings.Index(lower, "<title")
	if idx < 0 {
		return "", false
	}
	rest := html[idx:]
	gt := strings.Index(rest, ">")
	if gt < 0 {
		return "", false
	}
	contentStart := idx + gt + 1
	closeRel := strings.Index(strings.ToLower(html[contentStart:]), "</title>")
	if closeRel < 0 {
		return "", false
	}
	raw := html[contentStart : contentStart+closeRel]
	title := strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
	return title, title != ""
}

func truncateBrief(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return "..."
	}
	return string(r[:max-3]) + "..."
}
