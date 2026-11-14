package artifacts

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// A small XML tree that writes back exactly what it read: names keep their
// prefixes, attributes keep their order, namespace declarations stay as
// attributes. encoding/xml's Unmarshal rewrites prefixes, which would corrupt a
// Word document, so this reads raw tokens.

type xnode struct {
	prefix, local string // as written: <w:p> is prefix "w", local "p"
	attrs         []xml.Attr
	kids          []*xnode
	text          string // character data, for a text node
	raw           string // a processing instruction, comment or directive, verbatim
	isText        bool
}

func (n *xnode) name() string {
	if n.prefix == "" {
		return n.local
	}
	return n.prefix + ":" + n.local
}

func parseXML(b []byte) (*xnode, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	root := &xnode{}
	stack := []*xnode{root}
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{prefix: t.Name.Space, local: t.Name.Local, attrs: append([]xml.Attr(nil), t.Attr...)}
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			top.kids = append(top.kids, &xnode{isText: true, text: string(t)})
		case xml.ProcInst:
			top.kids = append(top.kids, &xnode{raw: "<?" + t.Target + " " + string(t.Inst) + "?>"})
		case xml.Comment:
			top.kids = append(top.kids, &xnode{raw: "<!--" + string(t) + "-->"})
		case xml.Directive:
			top.kids = append(top.kids, &xnode{raw: "<!" + string(t) + ">"})
		}
	}
	return root, nil
}

func escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (n *xnode) write(b *bytes.Buffer) {
	switch {
	case n.raw != "":
		b.WriteString(n.raw)
	case n.isText:
		b.WriteString(escape(n.text))
	case n.local == "" && n.prefix == "": // the document root
		for _, k := range n.kids {
			k.write(b)
		}
	default:
		b.WriteString("<" + n.name())
		for _, a := range n.attrs {
			name := a.Name.Local
			if a.Name.Space != "" {
				name = a.Name.Space + ":" + name
			}
			b.WriteString(" " + name + `="` + escape(a.Value) + `"`)
		}
		if len(n.kids) == 0 {
			b.WriteString("/>")
			return
		}
		b.WriteString(">")
		for _, k := range n.kids {
			k.write(b)
		}
		b.WriteString("</" + n.name() + ">")
	}
}

func (n *xnode) bytes() []byte {
	var b bytes.Buffer
	n.write(&b)
	return b.Bytes()
}

func (n *xnode) clone() *xnode {
	c := *n
	c.attrs = append([]xml.Attr(nil), n.attrs...)
	c.kids = make([]*xnode, len(n.kids))
	for i, k := range n.kids {
		c.kids[i] = k.clone()
	}
	return &c
}

// children returns the child elements with the given name (e.g. "w:tc").
func (n *xnode) children(name string) []*xnode {
	var out []*xnode
	for _, k := range n.kids {
		if !k.isText && k.raw == "" && k.name() == name {
			out = append(out, k)
		}
	}
	return out
}

func (n *xnode) child(name string) *xnode {
	if c := n.children(name); len(c) > 0 {
		return c[0]
	}
	return nil
}

// find returns every descendant element with the given name, in document
// order, without looking inside the ones it returns.
func (n *xnode) find(name string) []*xnode {
	var out []*xnode
	for _, k := range n.kids {
		if k.isText || k.raw != "" {
			continue
		}
		if k.name() == name {
			out = append(out, k)
			continue
		}
		out = append(out, k.find(name)...)
	}
	return out
}

// plainText joins the text of every w:t below n; paragraphs are separated by newlines.
func (n *xnode) plainText() string {
	var parts []string
	for _, p := range n.find("w:p") {
		var b strings.Builder
		var walk func(*xnode)
		walk = func(x *xnode) {
			for _, k := range x.kids {
				switch {
				case k.isText || k.raw != "":
				case k.name() == "w:t":
					for _, c := range k.kids {
						if c.isText {
							b.WriteString(c.text)
						}
					}
				case k.name() == "w:tab":
					b.WriteString("\t")
				default:
					walk(k)
				}
			}
		}
		walk(p)
		parts = append(parts, b.String())
	}
	return strings.Join(parts, "\n")
}

func elem(name string, kids ...*xnode) *xnode {
	prefix, local, _ := strings.Cut(name, ":")
	return &xnode{prefix: prefix, local: local, kids: kids}
}

func (n *xnode) attr(name, value string) *xnode {
	prefix, local, ok := strings.Cut(name, ":")
	a := xml.Attr{Name: xml.Name{Local: name}, Value: value}
	if ok {
		a.Name = xml.Name{Space: prefix, Local: local}
	}
	n.attrs = append(n.attrs, a)
	return n
}

func textNode(s string) *xnode { return &xnode{isText: true, text: s} }
