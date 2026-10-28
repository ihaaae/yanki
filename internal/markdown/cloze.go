package markdown

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// Inside math and code, Markdown strikethrough does not apply, so a `~~text~~`
// pair there is read directly as a cloze. Anki supports clozes inside MathJax
// and inside <code>/<pre>, which lets a cloze hide part of a formula or a line
// of code: `$\log_n(c^m) = ~~m~~ \log_n(~~c~~)$`.

// implicitClozeNumber marks a cloze whose number numberClozes assigns, so that
// strikethrough, math, and code clozes are numbered together in document order.
const implicitClozeNumber = "\ue000"

var (
	innerClozeRegex  = regexp.MustCompile(`~~(.+?)~~`)
	codeElementRegex = regexp.MustCompile(`(?s)(<code[^>]*>)(.*?)(</code>)`)
	clozeMarkerRegex = regexp.MustCompile(`\{\{c(\d+|` + implicitClozeNumber + `)::`)
	// Braces and colons in cloze content would end the cloze (`}}`) or start a
	// hint (`::`) early. As entities they still read as text, and the browser
	// decodes them before MathJax typesets the formula.
	clozeContentEscaper = strings.NewReplacer("{", "&#123;", "}", "&#125;", ":", "&#58;")
)

// replaceInnerClozes turns every `~~text~~` pair in already-escaped math or code
// text into Anki cloze markup. A leading number sets the cloze number, as with
// strikethrough clozes: `~~1 m~~`.
func replaceInnerClozes(text string) string {
	return innerClozeRegex.ReplaceAllStringFunc(text, func(match string) string {
		content := innerClozeRegex.FindStringSubmatch(match)[1]
		label := implicitClozeNumber

		if prefix := clozeNumberPrefixRegex.FindStringSubmatch(content); prefix != nil && strings.TrimSpace(prefix[2]) != "" {
			label = prefix[1]
			content = prefix[2]
		}

		return "{{c" + label + "::" + clozeContentEscaper.Replace(strings.TrimSpace(content)) + "}}"
	})
}

// clozeCode applies replaceInnerClozes to the content of every code element.
func clozeCode(rendered string) string {
	return codeElementRegex.ReplaceAllStringFunc(rendered, func(match string) string {
		parts := codeElementRegex.FindStringSubmatch(match)
		return parts[1] + replaceInnerClozes(parts[2]) + parts[3]
	})
}

// numberClozes assigns numbers to implicit clozes: each one takes the number
// after the previous cloze's.
func numberClozes(rendered string) string {
	next := 1

	return clozeMarkerRegex.ReplaceAllStringFunc(rendered, func(match string) string {
		label := clozeMarkerRegex.FindStringSubmatch(match)[1]
		if label == implicitClozeNumber {
			label = strconv.Itoa(next)
		}

		number, _ := strconv.Atoi(label)
		next = number + 1

		return "{{c" + label + "::"
	})
}

// hasInnerCloze reports whether a node is code, or text carrying math, that
// contains a `~~text~~` cloze.
func hasInnerCloze(node ast.Node, source []byte, mathSpans []string) bool {
	switch typed := node.(type) {
	case *ast.CodeSpan:
		var content strings.Builder
		for child := typed.FirstChild(); child != nil; child = child.NextSibling() {
			if value, ok := textValue(child, source); ok {
				content.WriteString(value)
			}
		}

		return innerClozeRegex.MatchString(content.String())

	case *ast.FencedCodeBlock, *ast.CodeBlock:
		lines := node.Lines()
		for index := 0; index < lines.Len(); index++ {
			segment := lines.At(index)
			if innerClozeRegex.Match(segment.Value(source)) {
				return true
			}
		}

	case *ast.Text:
		for _, match := range mathPlaceholderRegex.FindAllStringSubmatch(string(typed.Value(source)), -1) {
			if index, err := strconv.Atoi(match[1]); err == nil && index < len(mathSpans) && innerClozeRegex.MatchString(mathSpans[index]) {
				return true
			}
		}
	}

	return false
}
