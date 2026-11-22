package csl

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func isPunct(r rune) bool { return strings.ContainsRune(".,;:!?", r) }

// tidy turns straight quotes into curly ones, collapses colliding punctuation where one piece of
// text meets the next, and moves punctuation into closing quotes when the locale asks for it.
func tidy(toks []tok, q quotes, punctInQuote bool) []tok {
	toks = smartQuotes(toks, q)
	collide(toks)
	if punctInQuote {
		toks = moveIntoQuotes(toks)
		collide(toks)
	}
	return toks
}

// smartQuotes replaces straight quotes and apostrophes in the text with the locale's quotes,
// choosing outer or inner by nesting. Quotes it makes count as quotes for moving punctuation.
func smartQuotes(toks []tok, q quotes) []tok {
	var stack []byte // 0 for a quote the style added, otherwise the straight character
	prev := ' '
	out := make([]tok, 0, len(toks))
	for _, t := range toks {
		if t.kind != 't' {
			out = append(out, t)
			continue
		}
		switch {
		case t.quoteOpen:
			stack = append(stack, 0)
		case t.quoteClose:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case strings.ContainsAny(t.s, "\"'"):
			rs := []rune(t.s)
			var b strings.Builder
			flush := func() {
				if b.Len() > 0 {
					out = append(out, tok{kind: 't', s: b.String()})
					b.Reset()
				}
			}
			for j, c := range rs {
				if c != '"' && c != '\'' {
					b.WriteRune(c)
					prev = c
					continue
				}
				next := rune(0)
				if j+1 < len(rs) {
					next = rs[j+1]
				}
				s, ns, open, closing := straightQuote(c, prev, next, stack, q)
				stack = ns
				if open || closing {
					flush()
					out = append(out, tok{kind: 't', s: s, quoteOpen: open, quoteClose: closing})
				} else {
					b.WriteString(s)
				}
				prev, _ = utf8.DecodeLastRuneInString(s)
			}
			flush()
			continue
		}
		if t.s != "" {
			prev, _ = utf8.DecodeLastRuneInString(t.s)
		}
		out = append(out, t)
	}
	return out
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func straightQuote(c, prev, next rune, stack []byte, q quotes) (s string, ns []byte, open, closing bool) {
	opening := prev == ' ' || prev == '(' || prev == '[' || prev == '\u00a0' || prev == '\n' || prev == '“' || prev == '‘'
	top := byte(255)
	if len(stack) > 0 {
		top = stack[len(stack)-1]
	}
	if c == '\'' {
		switch {
		case wordRune(prev) && wordRune(next):
			return "’", stack, false, false
		case opening && unicode.IsDigit(next):
			return "’", stack, false, false
		case top == '\'' && !opening:
			return closeFor(len(stack)-1, q), stack[:len(stack)-1], false, true
		case !opening:
			return "’", stack, false, false
		}
		return openFor(len(stack), q), append(stack, '\''), true, false
	}
	if top == '"' && !opening {
		return closeFor(len(stack)-1, q), stack[:len(stack)-1], false, true
	}
	if opening || (next != 0 && !unicode.IsSpace(next) && !strings.ContainsRune(".,;:!?)]", next) && !unicode.IsLetter(prev)) {
		return openFor(len(stack), q), append(stack, '"'), true, false
	}
	if len(stack) > 0 {
		return closeFor(len(stack)-1, q), stack[:len(stack)-1], false, true
	}
	return openFor(len(stack), q), append(stack, '"'), true, false
}

func openFor(depth int, q quotes) string {
	if depth%2 == 0 {
		return q.open
	}
	return q.openInner
}

func closeFor(depth int, q quotes) string {
	if depth%2 == 0 {
		return q.close
	}
	return q.closeInner
}

// collide applies the punctuation rules where text meets text: a repeated mark is dropped; a
// colon, semicolon or period after a semicolon, exclamation or question mark is dropped; and a
// colon or semicolon before an exclamation or question mark is dropped.
func collide(toks []tok) {
	var last rune
	lastIdx := -1
	for i := range toks {
		t := &toks[i]
		if t.kind != 't' {
			continue
		}
		for t.s != "" {
			r, w := utf8.DecodeRuneInString(t.s)
			if r == ' ' && last == ' ' {
				t.s = t.s[w:]
				continue
			}
			if !isPunct(r) || !isPunct(last) {
				break
			}
			dropPrev, dropNext := collision(last, r)
			if dropPrev && lastIdx >= 0 {
				p := toks[lastIdx].s
				_, pw := utf8.DecodeLastRuneInString(p)
				toks[lastIdx].s = p[:len(p)-pw]
				last = lastRuneBefore(toks, lastIdx)
				continue
			}
			if !dropNext {
				break
			}
			t.s = t.s[w:]
		}
		if t.s != "" {
			last, _ = utf8.DecodeLastRuneInString(t.s)
			lastIdx = i
		}
	}
}

func lastRuneBefore(toks []tok, i int) rune {
	for ; i >= 0; i-- {
		if toks[i].kind == 't' && toks[i].s != "" {
			r, _ := utf8.DecodeLastRuneInString(toks[i].s)
			return r
		}
	}
	return 0
}

func collision(prev, next rune) (dropPrev, dropNext bool) {
	if prev == next {
		return false, true
	}
	switch next {
	case ':':
		return false, strings.ContainsRune(";!?", prev)
	case '.':
		return false, strings.ContainsRune(":;!?", prev)
	case '!', '?':
		return strings.ContainsRune(":;", prev), false
	}
	return false, false
}

func moveIntoQuotes(toks []tok) []tok {
	for i := range toks {
		if toks[i].kind != 't' || !toks[i].quoteClose {
			continue
		}
		for j := i + 1; j < len(toks); j++ {
			if toks[j].kind != 't' || toks[j].s == "" {
				continue
			}
			r, w := utf8.DecodeRuneInString(toks[j].s)
			if !strings.ContainsRune(".,!?", r) {
				break
			}
			toks[j].s = toks[j].s[w:]
			toks[i].s = string(r) + toks[i].s
		}
	}
	return toks
}
