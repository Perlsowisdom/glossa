package export

import (
	"strconv"
	"strings"

	"github.com/Perlsowisdom/glossa/internal/cite"
)

// Citations in a note or document are written [@source, p. 12] (package cite). Once the document is
// built they are rendered in its style, all together and in order, so a note style numbers its
// footnotes and works out ibid. and short forms, and a bibliography block becomes the list of the
// works cited. Every renderer then only has to draw footnote marks (Span.Foot), the footnotes
// (Doc.Notes) and ordinary text.

// eachSpans calls f on every run of spans in the document, in reading order, and keeps what it returns.
func (d *Doc) eachSpans(f func([]Span) []Span) {
	for pi := range d.Pieces {
		bs := d.Pieces[pi].Blocks
		for bi := range bs {
			b := &bs[bi]
			b.Spans = f(b.Spans)
			if b.Table == nil {
				continue
			}
			for i := range b.Table.HeadSpans {
				b.Table.HeadSpans[i] = f(b.Table.HeadSpans[i])
			}
			for ri := range b.Table.Rows {
				for ci := range b.Table.Rows[ri].Cells {
					b.Table.Rows[ri].Cells[ci] = f(b.Table.Rows[ri].Cells[ci])
				}
			}
		}
	}
}

func (d *Doc) hasBibliographyBlock() bool {
	for _, p := range d.Pieces {
		for _, b := range p.Blocks {
			if b.Kind == Bibliography {
				return true
			}
		}
	}
	return false
}

// runSpans turns a rendered citation's styled runs into spans, over the marks of the span it replaces.
func runSpans(runs []cite.Run, over Span) []Span {
	var out []Span
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		s := over
		s.Cite, s.Text = nil, r.Text
		s.Italic = s.Italic || r.Italic
		s.Bold = s.Bold || r.Bold
		s.Under = s.Under || r.Underline
		s.Super = s.Super || r.Super
		out = append(out, s)
	}
	return out
}

func (bd *builder) resolveCites(d *Doc) error {
	var cites []cite.Cite
	d.eachSpans(func(sp []Span) []Span {
		for _, s := range sp {
			if s.Cite != nil {
				cites = append(cites, *s.Cite)
			}
		}
		return sp
	})
	wantBib := d.Opt.Bibliography != "none"
	hasBlock := d.hasBibliographyBlock()
	if len(cites) == 0 {
		if hasBlock {
			d.dropBibliography(nil)
		}
		return nil
	}
	if bd.s.cites == nil {
		return nil
	}
	style := d.Opt.CiteStyle
	if style == "" {
		style = bd.s.cites.StyleFor(bd.ctx, bd.env, bd.docID)
	}
	res, err := bd.s.cites.Render(bd.ctx, bd.env, style, cites)
	if err != nil {
		return err
	}
	note := res.Style.Class == "note"
	i := 0
	d.eachSpans(func(sp []Span) []Span {
		var out []Span
		for _, s := range sp {
			if s.Cite == nil {
				out = append(out, s)
				continue
			}
			r := res.Citations[i]
			i++
			if note {
				d.Notes = append(d.Notes, Note{Num: r.NoteNumber, Spans: runSpans(r.Runs, Span{})})
				// The mark sits against the word before it, as in a word processor.
				if n := len(out); n > 0 && !out[n-1].Break && out[n-1].Foot == 0 {
					out[n-1].Text = strings.TrimRight(out[n-1].Text, " ")
				}
				out = append(out, Span{Text: strconv.Itoa(r.NoteNumber), Foot: r.NoteNumber, Super: true})
				continue
			}
			out = append(out, runSpans(r.Runs, s)...)
		}
		return out
	})
	var entries []Block
	if wantBib {
		for _, e := range res.Bibliography {
			entries = append(entries, Block{Kind: Para, Hang: true, Spans: runSpans(e.Runs, Span{})})
		}
	}
	title := "Bibliography"
	if !note {
		title = "References"
	}
	heading := Block{Kind: Heading, Level: 2, Spans: plain(title)}
	switch {
	case hasBlock && len(entries) == 0:
		d.dropBibliography(nil)
	case hasBlock:
		d.dropBibliography(append([]Block{heading}, entries...))
	case d.Opt.Bibliography == "end" && len(entries) > 0 && len(d.Pieces) > 0:
		last := &d.Pieces[len(d.Pieces)-1]
		last.Blocks = append(last.Blocks, append([]Block{heading}, entries...)...)
	}
	return nil
}

// dropBibliography replaces the first bibliography block with the given blocks (none removes it), and
// removes any other.
func (d *Doc) dropBibliography(with []Block) {
	first := true
	for pi := range d.Pieces {
		var out []Block
		for _, b := range d.Pieces[pi].Blocks {
			if b.Kind != Bibliography {
				out = append(out, b)
				continue
			}
			if first {
				out = append(out, with...)
				first = false
			}
		}
		d.Pieces[pi].Blocks = out
	}
}
