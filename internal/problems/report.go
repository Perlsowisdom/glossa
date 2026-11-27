package problems

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/Perlsowisdom/glossa/internal/command/core"
)

// Section headings of a report. The issue address is kept short by cutting the log, then the stack.
const (
	headNote    = "## What were you doing?"
	headAbout   = "## Glossa"
	headProblem = "## Problem"
	headLog     = "## Recent log"
	fence       = "```"
)

// IssueNewURL is the page for a new issue in Glossa's public repository. A report is only ever sent
// by the person opening this address in their browser and pressing Submit there.
const IssueNewURL = "https://github.com/Perlsowisdom/glossa/issues/new"

// MaxIssueURL is the longest address the issue page is given; longer ones are cut (shortenReport).
const MaxIssueURL = 7000

// logLines is how many recent log lines a report starts with.
const logLines = 200

// ReportOptions says what a report holds.
type ReportOptions struct {
	Note  string // what the person was doing, if they said
	Lines int    // recent log lines; 0 means the usual number
}

// Report builds the text of a report: the version, system, the latest problem if it is new, and the
// recent log. The text is the whole report; the person can change any of it before it goes anywhere.
func (s *Service) Report(o ReportOptions) string {
	if o.Lines <= 0 {
		o.Lines = logLines
	}
	roots := s.Roots()
	s.mu.Lock()
	webview := s.webview
	s.mu.Unlock()
	var b strings.Builder
	b.WriteString(headNote + "\n\n" + strings.TrimSpace(Strip(o.Note, roots)) + "\n\n")
	b.WriteString(headAbout + "\n\n")
	fmt.Fprintf(&b, "- Version: %s\n- System: %s\n", versionLine(core.Version), osLine())
	if webview != "" {
		fmt.Fprintf(&b, "- WebView2: %s\n", webview)
	}
	if p, ok := s.Latest(); ok && (!p.Reviewed || recent(p, s.now())) {
		b.WriteString("\n" + headProblem + "\n\n")
		fmt.Fprintf(&b, "%s (%s)\n", p.Summary, p.Kind)
		if p.Stack != "" {
			b.WriteString(fence + "\n" + p.Stack + "\n" + fence + "\n")
		}
	}
	b.WriteString("\n" + headLog + "\n\n" + fence + "\n")
	for _, line := range readLines(s.logDir, o.Lines) {
		b.WriteString(Strip(line, roots) + "\n")
	}
	b.WriteString(fence + "\n")
	return b.String()
}

func recent(p Problem, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, p.Time)
	return err == nil && now.Sub(t) < recentDays
}

// versionLine is the version with its channel: a development build, a test version (preview) or a full release.
func versionLine(v string) string {
	kind := "stable"
	switch {
	case strings.HasSuffix(v, "-dev"):
		kind = "development build"
	case strings.Contains(v, "-"):
		kind = "test version, preview channel"
	}
	return fmt.Sprintf("%s (%s, %s)", v, kind, distribution)
}

func osLine() string {
	name := osVersion()
	if name == "" {
		name = runtime.GOOS
	}
	return name + ", " + runtime.GOARCH
}

// ---- the issue address ------------------------------------------------------------------------

// IssueURL makes the address of a new issue holding text, and says whether text had to be cut to
// keep the address under MaxIssueURL. The log lines are cut first (the oldest go), then the stack,
// and only then the text as a whole.
func IssueURL(text string) (link string, cut bool) {
	title := issueTitle(text)
	build := func(body string) string {
		return IssueNewURL + "?title=" + escape(title) + "&body=" + escape(body)
	}
	if link = build(text); len(link) <= MaxIssueURL {
		return link, false
	}
	note := "\n_This report was shortened to fit a web address._\n"
	secs := splitSections(text)
	for _, head := range []string{headLog, headProblem} {
		i := indexOfSection(secs, head)
		if i < 0 {
			continue
		}
		body := secs[i].body
		keep := searchMax(len(body), func(k int) bool {
			trial := append([]section(nil), secs...)
			trial[i].body = keepBody(body, k, head == headLog)
			return len(build(joinSections(trial)+note)) <= MaxIssueURL
		})
		secs[i].body = keepBody(body, keep, head == headLog)
		if full := joinSections(secs) + note; len(build(full)) <= MaxIssueURL {
			return build(full), true
		}
	}
	full := joinSections(secs) + note
	runes := []rune(full)
	n := searchMax(len(runes), func(k int) bool { return len(build(string(runes[:k])+"…")) <= MaxIssueURL })
	return build(string(runes[:n]) + "…"), true
}

func escape(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// searchMax returns the largest k in [0, n] for which ok(k) holds, assuming ok is true for small k.
func searchMax(n int, ok func(int) bool) int {
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if ok(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

type section struct {
	head string   // the "## ..." line, empty for text before the first heading
	body []string // the lines under it
}

func splitSections(text string) []section {
	secs := []section{{}}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "## ") {
			secs = append(secs, section{head: line})
			continue
		}
		last := &secs[len(secs)-1]
		last.body = append(last.body, line)
	}
	return secs
}

func joinSections(secs []section) string {
	var lines []string
	for _, s := range secs {
		if s.head != "" {
			lines = append(lines, s.head)
		}
		lines = append(lines, s.body...)
	}
	return strings.Join(lines, "\n")
}

func indexOfSection(secs []section, head string) int {
	for i, s := range secs {
		if s.head == head {
			return i
		}
	}
	return -1
}

// keepBody keeps k of the lines between the code fences of a section: the newest k for a log (its
// oldest lines go first), the first k for a stack (its top frames matter most).
func keepBody(body []string, k int, newest bool) []string {
	var first, last = -1, -1
	for i, l := range body {
		if strings.HasPrefix(l, fence) {
			if first < 0 {
				first = i
			} else {
				last = i
			}
		}
	}
	if first < 0 || last < 0 { // no fences: treat every line as content
		first, last = -1, len(body)
	}
	content := body[first+1 : last]
	if k > len(content) {
		k = len(content)
	}
	if newest {
		content = content[len(content)-k:]
	} else {
		content = content[:k]
	}
	out := append([]string(nil), body[:first+1]...)
	out = append(out, content...)
	return append(out, body[last:]...)
}

// issueTitle is a short title: the problem's first line if the report has one, else a plain one.
func issueTitle(text string) string {
	secs := splitSections(text)
	if i := indexOfSection(secs, headProblem); i >= 0 {
		for _, l := range secs[i].body {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, fence) {
				return "Problem report: " + clip(l, 80)
			}
		}
	}
	return "Problem report"
}
