package markdown

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/ihaaae/yanki/internal/note"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Options controls how a Markdown document is parsed.
type Options struct {
	// Namespace is the Yanki namespace stamped into the note and used for
	// styling classes.
	Namespace string
	// StrictLineBreaks treats single newlines as line breaks when true. It is
	// accepted for API compatibility and currently always renders soft breaks.
	StrictLineBreaks bool

	// mathSpans holds the rendered math for each placeholder in the source.
	mathSpans []string
}

// clozeNumberPrefixRegex matches a leading cloze number and any content after
// it. Mirrors the original `^[\(\|]?(\d{1,2})(?:[\s\).\|]|$)(.*)$`.
var clozeNumberPrefixRegex = regexp.MustCompile(`^[(|]?(\d{1,2})(?:[\s).|]|$)(.*)$`)

// Parse converts a Markdown document into a note, inferring the Anki note type
// from the document's structure.
func Parse(markdown string, opts Options) (note.Note, error) {
	frontmatter, err := note.ParseFrontmatter(markdown)
	if err != nil {
		return note.Note{}, err
	}

	namespace, err := note.ValidateAndSanitizeNamespace(opts.Namespace)
	if err != nil {
		return note.Note{}, err
	}

	opts.Namespace = namespace

	md := selectParser(opts.StrictLineBreaks)

	protected, mathSpans := protectMath(note.StripFrontmatter(markdown))
	opts.mathSpans = mathSpans
	source := []byte(protected)
	parsed := md.Parser().Parse(text.NewReader(source))

	document, ok := parsed.(*ast.Document)
	if !ok {
		return note.Note{}, errors.New("markdown parser did not return a document")
	}

	modelName := inferModel(document, source, mathSpans)

	fields := note.Fields{YankiNamespace: namespace}

	switch modelName {
	case note.ModelBasic, note.ModelBasicReversed:
		first, second := splitAtThematicBreak(topLevelNodes(document))

		fields.Front, err = renderField(md, first, source, opts, modelName, "front", true)
		if err != nil {
			return note.Note{}, err
		}

		if second != nil {
			fields.Back, err = renderField(md, second, source, opts, modelName, "back", true)
			if err != nil {
				return note.Note{}, err
			}
		}

		if modelName == note.ModelBasicReversed && second != nil {
			newSecond, extra := splitAtThematicBreak(second)

			fields.Back, err = renderField(md, newSecond, source, opts, modelName, "back", true)
			if err != nil {
				return note.Note{}, err
			}

			if extra != nil {
				fields.Extra, err = renderField(md, extra, source, opts, modelName, "extra", false)
				if err != nil {
					return note.Note{}, err
				}
			}
		}

	case note.ModelTypeIn:
		answer := removeLastEmphasis(document)
		if answer == nil {
			return note.Note{}, errors.New("could not find emphasis in Basic (type in the answer) note")
		}

		fields.Front, err = renderField(md, topLevelNodes(document), source, opts, modelName, "front", true)
		if err != nil {
			return note.Note{}, err
		}

		answerParagraph := ast.NewParagraph()
		for _, child := range childNodes(answer) {
			answerParagraph.AppendChild(answerParagraph, child)
		}

		fields.Back, err = renderField(md, []ast.Node{answerParagraph}, source, opts, modelName, "back", false)
		if err != nil {
			return note.Note{}, err
		}

	case note.ModelCloze:
		first, second := splitAtThematicBreak(topLevelNodes(document))

		clozeify(first, source)

		fields.Front, err = renderField(md, first, source, opts, modelName, "front", true)
		if err != nil {
			return note.Note{}, err
		}

		if second != nil {
			fields.Back, err = renderField(md, second, source, opts, modelName, "back", false)
			if err != nil {
				return note.Note{}, err
			}
		}
	}

	return note.Note{
		DeckName:  frontmatter.DeckName,
		ModelName: modelName,
		Fields:    fields,
		NoteID:    frontmatter.NoteID,
		Tags:      tagsToAnkiTags(frontmatter.Tags),
	}, nil
}

// tagsToAnkiTags converts Obsidian's `/` tag hierarchy to Anki's `::`.
func tagsToAnkiTags(tags []string) []string {
	converted := make([]string, 0, len(tags))
	for _, tag := range tags {
		converted = append(converted, strings.ReplaceAll(tag, "/", "::"))
	}

	return converted
}

// topLevelNodes returns the document's block-level children.
func topLevelNodes(document *ast.Document) []ast.Node {
	var nodes []ast.Node
	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		nodes = append(nodes, child)
	}

	return nodes
}

// childNodes returns a node's direct children.
func childNodes(parent ast.Node) []ast.Node {
	var nodes []ast.Node
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		nodes = append(nodes, child)
	}

	return nodes
}

// splitAtThematicBreak splits block nodes at the first thematic break. A
// doubled break (two `---` in a row) is treated as one divider. It returns a
// nil second slice when there is no break at all, which callers treat as "no
// back field" rather than "empty back field".
func splitAtThematicBreak(nodes []ast.Node) (before, after []ast.Node) {
	for index, node := range nodes {
		if node.Kind() != ast.KindThematicBreak {
			continue
		}

		skip := 1
		if index+1 < len(nodes) && nodes[index+1].Kind() == ast.KindThematicBreak {
			skip = 2
		}

		return nodes[:index], nodes[index+skip:]
	}

	return nodes, nil
}

// inferModel determines which Anki note type a document describes.
func inferModel(document *ast.Document, source []byte, mathSpans []string) note.ModelName {
	if clozeBeforeBreak(document, source, mathSpans) {
		return note.ModelCloze
	}

	if !hasThematicBreak(document) && lastVisibleNodeIsEmphasisWithOthers(document, source) {
		return note.ModelTypeIn
	}

	probable := note.ModelName("")
	var last ast.Node

	for _, node := range topLevelNodes(document) {
		if node.Kind() == ast.KindThematicBreak {
			switch {
			case probable == "":
				probable = note.ModelBasic
			case probable == note.ModelBasic && last != nil && last.Kind() == ast.KindThematicBreak:
				return note.ModelBasicReversed
			}
		}

		last = node
	}

	if probable == "" {
		return note.ModelBasic
	}

	return probable
}

// clozeBeforeBreak reports whether a cloze appears before the first thematic
// break, which is what marks a document as a cloze note. A cloze is either a
// strikethrough or a `~~` pair inside math or code.
func clozeBeforeBreak(document *ast.Document, source []byte, mathSpans []string) bool {
	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() == ast.KindThematicBreak {
			return false
		}

		found := false
		_ = ast.Walk(child, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering && (node.Kind() == extast.KindStrikethrough || hasInnerCloze(node, source, mathSpans)) {
				found = true
				return ast.WalkStop, nil
			}

			return ast.WalkContinue, nil
		})

		if found {
			return true
		}
	}

	return false
}

// hasThematicBreak reports whether a document contains a thematic break.
func hasThematicBreak(document *ast.Document) bool {
	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Kind() == ast.KindThematicBreak {
			return true
		}
	}

	return false
}

// lastVisibleNodeIsEmphasisWithOthers reports whether the final visible node is
// emphasis and there is at least one other visible node, which is the
// type-in-the-answer signature.
func lastVisibleNodeIsEmphasisWithOthers(document *ast.Document, source []byte) bool {
	var lastVisible ast.Node
	visibleCount := 0

	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch typed := node.(type) {
		case *ast.Text:
			if strings.TrimSpace(string(typed.Value(source))) != "" {
				lastVisible = node
				visibleCount++
			}
		case *ast.String:
			if strings.TrimSpace(string(typed.Value)) != "" {
				lastVisible = node
				visibleCount++
			}
		case *ast.Emphasis:
			if emphasisHasVisibleText(typed, source) {
				lastVisible = node
				visibleCount++
				return ast.WalkSkipChildren, nil
			}
		}

		return ast.WalkContinue, nil
	})

	return lastVisible != nil && lastVisible.Kind() == ast.KindEmphasis && visibleCount > 1
}

// emphasisHasVisibleText reports whether an emphasis node contains non-blank
// text.
func emphasisHasVisibleText(emphasis *ast.Emphasis, source []byte) bool {
	for child := emphasis.FirstChild(); child != nil; child = child.NextSibling() {
		if value, ok := textValue(child, source); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}

	return false
}

// removeLastEmphasis detaches and returns the last emphasis node in document
// order.
func removeLastEmphasis(document *ast.Document) *ast.Emphasis {
	var last *ast.Emphasis

	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if emphasis, ok := node.(*ast.Emphasis); ok {
				last = emphasis
			}
		}

		return ast.WalkContinue, nil
	})

	if last == nil {
		return nil
	}

	if parent := last.Parent(); parent != nil {
		parent.RemoveChild(parent, last)
	}

	return last
}

// textValue returns a node's literal text when it is a text or string node.
func textValue(node ast.Node, source []byte) (string, bool) {
	switch typed := node.(type) {
	case *ast.Text:
		return string(typed.Value(source)), true
	case *ast.String:
		return string(typed.Value), true
	default:
		return "", false
	}
}

// clozeify rewrites every strikethrough node in the given block nodes into
// Anki's cloze markup.
//
// Nodes are collected before any mutation: replacing a node mid-walk would
// disturb the sibling links goldmark's walker relies on, and later clozes would
// be skipped.
func clozeify(nodes []ast.Node, source []byte) {
	var strikethroughs []*extast.Strikethrough

	for _, node := range nodes {
		_ = ast.Walk(node, func(current ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering {
				if strikethrough, ok := current.(*extast.Strikethrough); ok {
					strikethroughs = append(strikethroughs, strikethrough)
				}
			}

			return ast.WalkContinue, nil
		})
	}

	for _, strikethrough := range strikethroughs {
		parent := strikethrough.Parent()
		if parent == nil || !strikethrough.HasChildren() {
			continue
		}

		replacements := buildCloze(strikethrough, source)

		if len(replacements) == 0 {
			continue
		}

		parent.ReplaceChild(parent, strikethrough, replacements[0])
		previous := replacements[0]
		for _, replacement := range replacements[1:] {
			parent.InsertAfter(parent, previous, replacement)
			previous = replacement
		}
	}
}

// buildCloze converts one strikethrough node into the sequence of nodes that
// render as `{{cN::content::hint}}`. Without an explicit number it leaves an
// implicit-number marker for numberClozes.
func buildCloze(strikethrough *extast.Strikethrough, source []byte) []ast.Node {
	children := childNodes(strikethrough)
	label := implicitClozeNumber

	// A leading one- or two-digit number overrides the implicit index, but only
	// when there is other content to cloze.
	if len(children) > 0 {
		if value, ok := textValue(children[0], source); ok {
			if match := clozeNumberPrefixRegex.FindStringSubmatch(value); match != nil {
				remainder := match[2]
				if len(children) > 1 || strings.TrimSpace(remainder) != "" {
					if parsed, err := strconv.Atoi(match[1]); err == nil {
						label = strconv.Itoa(parsed)
						children = replaceFirstText(children, remainder)
					}
				}
			}
		}
	}

	var hint []ast.Node
	content := children
	if len(children) > 1 {
		if _, isEmphasis := children[len(children)-1].(*ast.Emphasis); isEmphasis {
			hint = children[len(children)-1:]
			content = children[:len(children)-1]
		}
	}

	nodes := []ast.Node{ast.NewString([]byte("{{c" + label + "::"))}
	nodes = append(nodes, trimEdges(content, source)...)

	if len(hint) > 0 {
		nodes = append(nodes, ast.NewString([]byte("::")))
		nodes = append(nodes, trimEdges(hint, source)...)
	}

	nodes = append(nodes, ast.NewString([]byte("}}")))

	return nodes
}

// replaceFirstText swaps the first text node for a string node carrying new
// content, so the cloze number can be stripped.
func replaceFirstText(nodes []ast.Node, value string) []ast.Node {
	result := make([]ast.Node, 0, len(nodes))
	if strings.TrimSpace(value) == "" {
		result = append(result, nodes[1:]...)
	} else {
		result = append(result, ast.NewString([]byte(value)))
		result = append(result, nodes[1:]...)
	}

	return result
}

// trimEdges trims surrounding whitespace from the first and last text nodes of
// a cloze, dropping them when they become empty.
func trimEdges(nodes []ast.Node, source []byte) []ast.Node {
	result := append([]ast.Node{}, nodes...)

	if len(result) > 0 {
		if value, ok := textValue(result[0], source); ok {
			trimmed := strings.TrimLeft(value, " \t")
			if trimmed == "" && len(result) > 1 {
				result = result[1:]
			} else {
				result[0] = ast.NewString([]byte(trimmed))
			}
		}
	}

	if len(result) > 0 {
		last := len(result) - 1
		if value, ok := textValue(result[last], source); ok {
			trimmed := strings.TrimRight(value, " \t")
			if trimmed == "" && len(result) > 1 {
				result = result[:last]
			} else {
				result[last] = ast.NewString([]byte(trimmed))
			}
		}
	}

	return result
}
