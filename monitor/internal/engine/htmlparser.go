//go:build solver

package engine

import (
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// parsedNode represents a parsed HTML node for JSON serialization to V8.
type parsedNode struct {
	Type     string            `json:"type"`               // "element", "text", "comment"
	Tag      string            `json:"tag,omitempty"`      // tag name (lowercase)
	Attrs    map[string]string `json:"attrs,omitempty"`    // attributes
	Children []*parsedNode     `json:"children,omitempty"` // child nodes
	Text     string            `json:"text,omitempty"`     // text content (for text/comment nodes)
}

// ParseHTML parses an HTML string and returns a JSON array of parsed nodes.
// This is called from V8 via __goParseHTML.
func ParseHTML(htmlStr string) string {
	// Use html.ParseFragment to handle partial HTML (no <html>/<body> wrapper needed)
	reader := strings.NewReader(htmlStr)
	nodes, err := html.ParseFragment(reader, &html.Node{
		Type:     html.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err != nil {
		// Fallback: try as full document
		reader = strings.NewReader(htmlStr)
		doc, err2 := html.Parse(reader)
		if err2 != nil {
			return "[]"
		}
		// Extract body children
		body := findBody(doc)
		if body == nil {
			return "[]"
		}
		var result []*parsedNode
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			if n := convertNode(c); n != nil {
				result = append(result, n)
			}
		}
		data, _ := json.Marshal(result)
		return string(data)
	}

	var result []*parsedNode
	for _, node := range nodes {
		if n := convertNode(node); n != nil {
			result = append(result, n)
		}
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "body" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findBody(c); found != nil {
			return found
		}
	}
	return nil
}

func convertNode(n *html.Node) *parsedNode {
	switch n.Type {
	case html.ElementNode:
		pn := &parsedNode{
			Type: "element",
			Tag:  n.Data,
		}
		if len(n.Attr) > 0 {
			pn.Attrs = make(map[string]string, len(n.Attr))
			for _, a := range n.Attr {
				pn.Attrs[a.Key] = a.Val
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if cn := convertNode(c); cn != nil {
				pn.Children = append(pn.Children, cn)
			}
		}
		return pn
	case html.TextNode:
		return &parsedNode{Type: "text", Text: n.Data}
	case html.CommentNode:
		return &parsedNode{Type: "comment", Text: n.Data}
	default:
		return nil
	}
}
