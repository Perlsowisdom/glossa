package export

import (
	"fmt"
	"strings"
)

// Footnotes in a Word file are real footnotes (word/footnotes.xml): Word numbers them and puts them
// at the foot of the page.

func contentTypesFor(notes bool) string {
	if !notes {
		return contentTypesBase
	}
	return strings.Replace(contentTypesBase, "</Types>",
		`<Override PartName="/word/footnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"/></Types>`, 1)
}

func settingsFor(notes bool) string {
	if !notes {
		return settingsXML
	}
	return strings.Replace(settingsXML, `<w:compat>`, `<w:footnotePr><w:footnote w:id="-1"/><w:footnote w:id="0"/></w:footnotePr><w:compat>`, 1)
}

func footRef(n int) string {
	return fmt.Sprintf(`<w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:footnoteReference w:id="%d"/></w:r>`, n)
}

func footnotesXML(d Doc, _ *relList) string {
	if len(d.Notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(xmlHead + `<w:footnotes ` + wNS + `>`)
	b.WriteString(`<w:footnote w:type="separator" w:id="-1"><w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:separator/></w:r></w:p></w:footnote>`)
	b.WriteString(`<w:footnote w:type="continuationSeparator" w:id="0"><w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:continuationSeparator/></w:r></w:p></w:footnote>`)
	for _, n := range d.Notes {
		fmt.Fprintf(&b, `<w:footnote w:id="%d"><w:p><w:pPr><w:pStyle w:val="FootnoteText"/></w:pPr>`+
			`<w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:footnoteRef/></w:r>%s%s</w:p></w:footnote>`,
			n.Num, run(" ", runStyle{}), runs(n.Spans, nil))
	}
	b.WriteString(`</w:footnotes>`)
	return b.String()
}

// The styles the footnotes and the bibliography use.
const notesStyles = `<w:style w:type="paragraph" w:styleId="FootnoteText"><w:name w:val="footnote text"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="40" w:line="240" w:lineRule="auto"/></w:pPr><w:rPr><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:styleId="FootnoteReference"><w:name w:val="footnote reference"/><w:qFormat/><w:rPr><w:vertAlign w:val="superscript"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Hang"><w:name w:val="Bibliography"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:ind w:left="567" w:hanging="567"/><w:spacing w:after="80"/></w:pPr></w:style>`
