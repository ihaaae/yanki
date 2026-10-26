package markdown

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Math in notes uses Obsidian's `$inline$` and `$$display$$` delimiters, while
// Anki's MathJax only recognizes `\(inline\)` and `\[display\]`. Math is lifted
// out of the source before Markdown parsing, so emphasis and escape rules
// cannot mangle it (`a_1 * b_2` is not emphasis), and is restored with Anki's
// delimiters after rendering.

// Placeholders are wrapped in Unicode punctuation so that, like the math they
// stand for, they do not stop adjacent emphasis from opening or closing:
// `$y$**-axis**` must still be bold.
const (
	mathPlaceholderOpen  = "\u27e6yankimath"
	mathPlaceholderClose = "\u27e7"
)

var mathPlaceholderRegex = regexp.MustCompile(mathPlaceholderOpen + `(\d+)` + mathPlaceholderClose)

var mathEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// protectMath replaces math spans outside code with placeholders and returns
// the rewritten source with the rendered math for each placeholder.
//
// Delimiters follow Pandoc's rules, which keep shell and AWK snippets such as
// `$1 and $2` from being read as math: an opening `$` must not be followed by
// whitespace, and a closing `$` must not be preceded by whitespace or followed
// by a digit. Inline math cannot span a blank line.
func protectMath(source string) (string, []string) {
	var (
		out     strings.Builder
		spans   []string
		inFence string
	)

	lines := strings.SplitAfter(source, "\n")

	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimLeft(line, " ")

		if inFence != "" {
			if strings.HasPrefix(trimmed, inFence) {
				inFence = ""
			}

			out.WriteString(line)
			continue
		}

		if fence := fenceMarker(trimmed); fence != "" {
			inFence = fence
			out.WriteString(line)
			continue
		}

		// Join the rest of the paragraph so display and inline math may span
		// lines, but never across a blank line or into a code fence.
		paragraph := line
		for index+1 < len(lines) && strings.TrimSpace(lines[index+1]) != "" && fenceMarker(strings.TrimLeft(lines[index+1], " ")) == "" && strings.TrimSpace(line) != "" {
			index++
			paragraph += lines[index]
		}

		out.WriteString(protectMathInParagraph(paragraph, &spans))
	}

	return out.String(), spans
}

func fenceMarker(trimmed string) string {
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			return marker
		}
	}

	return ""
}

func protectMathInParagraph(text string, spans *[]string) string {
	var out strings.Builder

	placeholder := func(rendered string) {
		fmt.Fprintf(&out, "%s%d%s", mathPlaceholderOpen, len(*spans), mathPlaceholderClose)
		*spans = append(*spans, rendered)
	}

	for index := 0; index < len(text); {
		switch character := text[index]; {
		case character == '\\' && index+1 < len(text):
			out.WriteString(text[index : index+2])
			index += 2

		case character == '`':
			end := codeSpanEnd(text, index)
			out.WriteString(text[index:end])
			index = end

		case strings.HasPrefix(text[index:], "$$"):
			closing := strings.Index(text[index+2:], "$$")
			if closing <= 0 {
				out.WriteString("$$")
				index += 2
				continue
			}

			body := text[index+2 : index+2+closing]
			placeholder(`\[` + mathEscaper.Replace(strings.TrimSpace(body)) + `\]`)
			index += 2 + closing + 2

		case character == '$':
			end := inlineMathEnd(text, index)
			if end == -1 {
				out.WriteByte('$')
				index++
				continue
			}

			placeholder(`\(` + mathEscaper.Replace(text[index+1:end]) + `\)`)
			index = end + 1

		default:
			out.WriteByte(character)
			index++
		}
	}

	return out.String()
}

// codeSpanEnd returns the index just past the code span starting at start, or
// just past the opening backticks when the span is never closed.
func codeSpanEnd(text string, start int) int {
	run := start
	for run < len(text) && text[run] == '`' {
		run++
	}

	fence := text[start:run]
	if closing := strings.Index(text[run:], fence); closing != -1 {
		return run + closing + len(fence)
	}

	return run
}

// inlineMathEnd returns the index of the `$` closing the inline math opened at
// start, or -1 when the `$` does not open math.
func inlineMathEnd(text string, start int) int {
	if start+1 >= len(text) || isSpace(text[start+1]) {
		return -1
	}

	for index := start + 1; index < len(text); index++ {
		switch text[index] {
		case '\\':
			index++
		case '\n':
			if index+1 < len(text) && text[index+1] == '\n' {
				return -1
			}
		case '$':
			if isSpace(text[index-1]) {
				continue
			}

			if index+1 < len(text) && text[index+1] >= '0' && text[index+1] <= '9' {
				continue
			}

			return index
		}
	}

	return -1
}

func isSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r'
}

// restoreMath swaps placeholders in rendered HTML for Anki MathJax markup.
func restoreMath(rendered string, spans []string) string {
	if len(spans) == 0 {
		return rendered
	}

	return mathPlaceholderRegex.ReplaceAllStringFunc(rendered, func(match string) string {
		index, err := strconv.Atoi(mathPlaceholderRegex.FindStringSubmatch(match)[1])
		if err != nil || index >= len(spans) {
			return match
		}

		return spans[index]
	})
}
