package csl

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// xnode is a generic XML element. Styles and locales are kept as trees of these and interpreted
// while rendering, which keeps the parser small and lets every attribute reach the renderer.
type xnode struct {
	Name string
	Attr map[string]string
	Kids []*xnode
	Text string // character data directly inside the element
}

func (n *xnode) a(k string) string {
	if n == nil {
		return ""
	}
	return n.Attr[k]
}

func (n *xnode) has(k string) bool {
	if n == nil {
		return false
	}
	_, ok := n.Attr[k]
	return ok
}

// child returns the first child with the given element name.
func (n *xnode) child(name string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Name == name {
			return k
		}
	}
	return nil
}

func (n *xnode) children(name string) []*xnode {
	var out []*xnode
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Name == name {
			out = append(out, k)
		}
	}
	return out
}

func parseXML(r io.Reader) (*xnode, error) {
	d := xml.NewDecoder(r)
	d.Strict = false
	d.Entity = xml.HTMLEntity
	var stack []*xnode
	var root *xnode
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csl: bad XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{Name: t.Name.Local, Attr: map[string]string{}}
			for _, at := range t.Attr {
				key := at.Name.Local
				if at.Name.Space == "xml" || at.Name.Space == "http://www.w3.org/XML/1998/namespace" {
					key = "xml:" + key
				} else if at.Name.Space == "xmlns" || key == "xmlns" {
					continue
				}
				n.Attr[key] = at.Value
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Kids = append(p.Kids, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("csl: empty XML document")
	}
	return root, nil
}

func fields(s string) []string { return strings.Fields(s) }
