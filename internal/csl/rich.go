package csl

import (
	"strings"
	"unicode/utf8"
)

// rich is the rendered-text tree the engine builds: text with font styling, quotes and display
// blocks. Exporters read Output.Runs; the tree itself is internal to rendering.
type rich struct {
	Text string
	Kids []*rich

	Style, Weight, Variant, Deco, VAlign string // font-style, font-weight, font-variant, text-decoration, vertical-align
	Quote                                bool
	Display                              string // block, left-margin, right-inline, indent
	NoCase, NoDecor                      bool
	Tag                                  string // internal marker: "names", "year", ...
	nameParts                            []*rich
}

func leaf(s string) *rich { return &rich{Text: s} }

func (n *rich) styled() bool {
	return n.Style != "" || n.Weight != "" || n.Variant != "" || n.Deco != "" || n.VAlign != "" ||
		n.Display != "" || n.NoDecor
}

func (n *rich) empty() bool {
	if n == nil {
		return true
	}
	if n.Text != "" {
		return false
	}
	for _, k := range n.Kids {
		if !k.empty() {
			return false
		}
	}
	return true
}

func allEmpty(ns []*rich) bool {
	for _, n := range ns {
		if !n.empty() {
			return false
		}
	}
	return true
}

// plain is the text of a subtree with no quotes added.
func (n *rich) plain() string {
	var b strings.Builder
	n.writePlain(&b)
	return b.String()
}

func (n *rich) writePlain(b *strings.Builder) {
	b.WriteString(n.Text)
	for _, k := range n.Kids {
		k.writePlain(b)
	}
}

func plainOf(ns []*rich) string {
	var b strings.Builder
	for _, n := range ns {
		n.writePlain(&b)
	}
	return b.String()
}

// Run is a stretch of text with one set of styling, the form exporters map from.
type Run struct {
	Text                                           string
	Italic, Bold, SmallCaps, Underline, Super, Sub bool
	Display                                        string
}

// Output is rendered text in the three forms callers need.
type Output struct {
	Text string // plain text
	HTML string
	Runs []Run
}

// IsEmpty is true when nothing was rendered.
func (o Output) IsEmpty() bool { return o.Text == "" }

// quotes holds the terms used to quote text.
type quotes struct{ open, close, openInner, closeInner string }

type tok struct {
	kind       byte // 't' text, 'o' open, 'c' close
	n          *rich
	s          string
	quoteClose bool
	eff        effStyle
}

type effStyle struct{ style, weight, variant, deco, valign string }

type flattener struct {
	q     quotes
	depth int
	toks  []tok
	stack []effStyle
}

func (f *flattener) cur() effStyle {
	if len(f.stack) == 0 {
		return effStyle{}
	}
	return f.stack[len(f.stack)-1]
}

func (f *flattener) walk(n *rich) {
	if n.Quote {
		f.depth++
		if f.depth%2 == 1 {
			f.toks = append(f.toks, tok{kind: 't', s: f.q.open})
		} else {
			f.toks = append(f.toks, tok{kind: 't', s: f.q.openInner})
		}
	}
	opened := n.styled()
	closers := ""
	if opened {
		parent := f.cur()
		e := parent
		e.style = toggle(n.Style, parent.style, "italic")
		e.weight = toggle(n.Weight, parent.weight, "bold")
		e.variant = toggle(n.Variant, parent.variant, "small-caps")
		if n.Deco != "" {
			e.deco = n.Deco
		}
		if n.VAlign != "" {
			e.valign = n.VAlign
		}
		f.stack = append(f.stack, e)
		openS, closeS := tagsFor(n, parent, e)
		closers = closeS
		f.toks = append(f.toks, tok{kind: 'o', n: n, eff: e, s: openS})
	}
	if n.Text != "" {
		f.toks = append(f.toks, tok{kind: 't', s: n.Text})
	}
	for _, k := range n.Kids {
		f.walk(k)
	}
	if opened {
		e := f.cur()
		f.stack = f.stack[:len(f.stack)-1]
		f.toks = append(f.toks, tok{kind: 'c', n: n, eff: e, s: closers})
	}
	if n.Quote {
		if f.depth%2 == 1 {
			f.toks = append(f.toks, tok{kind: 't', s: f.q.close, quoteClose: true})
		} else {
			f.toks = append(f.toks, tok{kind: 't', s: f.q.closeInner, quoteClose: true})
		}
		f.depth--
	}
}

// toggle applies a font value: setting the style that is already active turns it off.
func toggle(v, inherited, on string) string {
	switch v {
	case "":
		return inherited
	case on:
		if inherited == on {
			return "normal"
		}
		return on
	}
	return v
}

func isPunct(r rune) bool { return strings.ContainsRune(".,;:!?", r) }

// tidy moves punctuation into closing quotes (when asked) and removes doubled punctuation and
// spaces where one piece of text meets the next.
func tidy(toks []tok, punctInQuote bool) []tok {
	if punctInQuote {
		toks = moveIntoQuotes(toks)
	}
	var last rune
	for i := range toks {
		t := &toks[i]
		if t.kind != 't' {
			continue
		}
		s := t.s
		for s != "" {
			r, w := utf8.DecodeRuneInString(s)
			drop := false
			switch {
			case last == ' ' && r == ' ':
				drop = true
			case isPunct(r) && r == last:
				drop = true
			case r == '.' && (last == '?' || last == '!'):
				drop = true
			}
			if !drop {
				break
			}
			s = s[w:]
		}
		t.s = s
		if s != "" {
			last, _ = utf8.DecodeLastRuneInString(s)
		}
	}
	return toks
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
			if r == '.' || r == ',' {
				toks[j].s = toks[j].s[w:]
				toks[i].s = string(r) + toks[i].s
			}
			break
		}
	}
	return toks
}

func finalize(root []*rich, q quotes, punctInQuote bool) Output {
	f := &flattener{q: q}
	for _, n := range root {
		f.walk(n)
	}
	toks := tidy(f.toks, punctInQuote)
	return Output{Text: textOf(toks), HTML: htmlOf(toks), Runs: runsOf(toks)}
}

func textOf(toks []tok) string {
	var b strings.Builder
	for _, t := range toks {
		if t.kind == 't' {
			b.WriteString(t.s)
		}
	}
	return b.String()
}

var htmlEscaper = strings.NewReplacer("&", "&#38;", "<", "&#60;", ">", "&#62;")

func escHTML(s string) string {
	if !strings.ContainsAny(s, "&<>") {
		return s
	}
	return htmlEscaper.Replace(s)
}

// tagsFor gives the HTML that opens and closes a styled node. A font value only produces markup
// when it changes what the surrounding text already has.
func tagsFor(n *rich, parent, e effStyle) (open, closeS string) {
	add := func(o, c string) { open += o; closeS = c + closeS }
	if n.Display != "" {
		add("<div class=\"csl-"+n.Display+"\">", "</div>")
	}
	switch {
	case n.Style == "italic" && e.style == "italic":
		add("<i>", "</i>")
	case n.Style == "normal" && parent.style == "italic":
		add("<span style=\"font-style:normal;\">", "</span>")
	case n.Style == "oblique":
		add("<span style=\"font-style:oblique;\">", "</span>")
	}
	switch {
	case n.Weight == "bold" && e.weight == "bold":
		add("<b>", "</b>")
	case n.Weight == "normal" && parent.weight == "bold":
		add("<span style=\"font-weight:normal;\">", "</span>")
	case n.Weight == "light":
		add("<span style=\"font-weight:light;\">", "</span>")
	}
	switch {
	case n.Variant == "small-caps" && e.variant == "small-caps":
		add("<span style=\"font-variant:small-caps;\">", "</span>")
	case n.Variant == "normal" && parent.variant == "small-caps":
		add("<span style=\"font-variant:normal;\">", "</span>")
	}
	if n.Deco == "underline" {
		add("<span style=\"text-decoration:underline;\">", "</span>")
	}
	switch n.VAlign {
	case "sup":
		add("<sup>", "</sup>")
	case "sub":
		add("<sub>", "</sub>")
	}
	if n.NoDecor {
		add("<span class=\"nodecor\" style=\"font-variant:normal;font-style:normal;font-weight:normal;text-decoration:none;\">", "</span>")
	}
	return open, closeS
}

func htmlOf(toks []tok) string {
	var b strings.Builder
	for _, t := range toks {
		switch t.kind {
		case 't':
			b.WriteString(escHTML(t.s))
		case 'o', 'c':
			b.WriteString(t.s)
		}
	}
	return b.String()
}

func runsOf(toks []tok) []Run {
	var runs []Run
	var stack []tok
	disp := func() string {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].n.Display != "" {
				return stack[i].n.Display
			}
		}
		return ""
	}
	for _, t := range toks {
		switch t.kind {
		case 'o':
			stack = append(stack, t)
		case 'c':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case 't':
			if t.s == "" {
				continue
			}
			var e effStyle
			if len(stack) > 0 {
				e = stack[len(stack)-1].eff
			}
			r := Run{Text: t.s, Italic: e.style == "italic" || e.style == "oblique", Bold: e.weight == "bold",
				SmallCaps: e.variant == "small-caps", Underline: e.deco == "underline",
				Super: e.valign == "sup", Sub: e.valign == "sub", Display: disp()}
			if n := len(runs); n > 0 {
				p := runs[n-1]
				if p.Italic == r.Italic && p.Bold == r.Bold && p.SmallCaps == r.SmallCaps && p.Underline == r.Underline &&
					p.Super == r.Super && p.Sub == r.Sub && p.Display == r.Display {
					runs[n-1].Text += r.Text
					continue
				}
			}
			runs = append(runs, r)
		}
	}
	return runs
}

// parseMarkup turns the inline HTML a field may carry (<i>, <b>, <sup>, <sub>, small-caps and
// no-case spans) into rich nodes. Anything else is kept as text.
func parseMarkup(s string) []*rich {
	if !strings.Contains(s, "<") {
		return []*rich{leaf(s)}
	}
	root := &rich{}
	stack := []*rich{root}
	top := func() *rich { return stack[len(stack)-1] }
	for len(s) > 0 {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			top().Kids = append(top().Kids, leaf(s))
			break
		}
		if i > 0 {
			top().Kids = append(top().Kids, leaf(s[:i]))
			s = s[i:]
		}
		j := strings.IndexByte(s, '>')
		if j < 0 {
			top().Kids = append(top().Kids, leaf(s))
			break
		}
		if n, closing, ok := markupTag(s[1:j]); ok {
			if closing {
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
			} else {
				top().Kids = append(top().Kids, n)
				stack = append(stack, n)
			}
		} else {
			top().Kids = append(top().Kids, leaf(s[:j+1]))
		}
		s = s[j+1:]
	}
	return root.Kids
}

func markupTag(tag string) (n *rich, closing, ok bool) {
	t := strings.ToLower(strings.TrimSpace(tag))
	if strings.HasPrefix(t, "/") {
		switch strings.TrimSpace(t[1:]) {
		case "i", "b", "sup", "sub", "span", "u", "sc":
			return nil, true, true
		}
		return nil, false, false
	}
	switch {
	case t == "i":
		return &rich{Style: "italic"}, false, true
	case t == "b":
		return &rich{Weight: "bold"}, false, true
	case t == "sup":
		return &rich{VAlign: "sup"}, false, true
	case t == "sub":
		return &rich{VAlign: "sub"}, false, true
	case t == "u":
		return &rich{Deco: "underline"}, false, true
	case t == "sc":
		return &rich{Variant: "small-caps"}, false, true
	case strings.HasPrefix(t, "span"):
		if strings.Contains(t, "nocase") {
			return &rich{NoCase: true}, false, true
		}
		if strings.Contains(t, "nodecor") {
			return &rich{NoDecor: true}, false, true
		}
		if strings.Contains(t, "small-caps") {
			return &rich{Variant: "small-caps"}, false, true
		}
		return &rich{}, false, true
	}
	return nil, false, false
}
