package note

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter is the subset of a note's YAML frontmatter that Yanki reads.
type Frontmatter struct {
	DeckName string
	// NoteID is the Anki note ID recorded in the frontmatter, or 0 when absent.
	NoteID int64
	// Priority is the 1-based priority tier, or 0 when absent.
	Priority int
	Tags     []string
}

var noteIDLineRegex = regexp.MustCompile(`^noteId\s*:\s*(-?\d+)?\s*$`)

// splitLines splits on both LF and CRLF, keeping the terminator decision to the
// caller via joinLines.
func splitLines(markdown string) []string {
	return strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
}

// frontmatterRange returns the line indexes of the opening and closing `---`
// delimiters, or ok=false when the document has no frontmatter.
func frontmatterRange(lines []string) (start, end int, ok bool) {
	if !strings.HasPrefix(strings.TrimLeft(strings.Join(lines, ""), " \t\r\n"), "---") {
		return 0, 0, false
	}

	start = -1
	for i, line := range lines {
		if strings.HasPrefix(line, "---") {
			start = i
			break
		}
	}

	if start == -1 {
		return 0, 0, false
	}

	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "---") {
			return start, i, true
		}
	}

	return 0, 0, false
}

// StripFrontmatter returns the Markdown body with any leading YAML frontmatter
// removed, so that parsing does not mistake the delimiters for content.
func StripFrontmatter(markdown string) string {
	lines := splitLines(markdown)
	_, end, ok := frontmatterRange(lines)
	if !ok {
		return markdown
	}

	return strings.Join(lines[end+1:], "\n")
}

// ParseFrontmatter extracts the Yanki-relevant frontmatter from a Markdown
// document. A document without frontmatter yields a zero Frontmatter.
func ParseFrontmatter(markdown string) (Frontmatter, error) {
	lines := splitLines(markdown)
	start, end, ok := frontmatterRange(lines)
	if !ok {
		return Frontmatter{}, nil
	}

	raw := strings.Join(lines[start+1:end], "\n")
	if strings.TrimSpace(raw) == "" {
		return Frontmatter{}, nil
	}

	var decoded map[string]any
	if err := yaml.Unmarshal([]byte(raw), &decoded); err != nil {
		return Frontmatter{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	frontmatter := Frontmatter{}

	if deckName, ok := decoded["deckName"].(string); ok {
		frontmatter.DeckName = deckName
	}

	switch noteID := decoded["noteId"].(type) {
	case int:
		frontmatter.NoteID = int64(noteID)
	case int64:
		frontmatter.NoteID = noteID
	case float64:
		frontmatter.NoteID = int64(noteID)
	case string:
		if parsed, err := strconv.ParseInt(noteID, 10, 64); err == nil {
			frontmatter.NoteID = parsed
		}
	}

	if priority, ok := decoded["priority"].(int); ok {
		frontmatter.Priority = priority
	}

	frontmatter.Tags = coerceTags(decoded["tags"])

	return frontmatter, nil
}

// coerceTags accepts the shapes Obsidian allows for a `tags` value: a list of
// strings, or a single string.
func coerceTags(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		tags := make([]string, 0, len(typed))
		for _, entry := range typed {
			tags = append(tags, fmt.Sprint(entry))
		}

		return tags
	case []string:
		return typed
	default:
		return []string{fmt.Sprint(typed)}
	}
}

// SetNoteID returns the Markdown with the frontmatter `noteId` set to noteID,
// or removed when noteID is nil. All other frontmatter and the document body
// are preserved verbatim.
func SetNoteID(markdown string, noteID *int64) (string, error) {
	lines := splitLines(markdown)
	start, end, ok := frontmatterRange(lines)

	if !ok {
		if noteID == nil {
			return markdown, nil
		}

		return strings.Join(append([]string{"---", fmt.Sprintf("noteId: %d", *noteID), "---", ""}, lines...), "\n"), nil
	}

	frontmatterLines := append([]string{}, lines[start+1:end]...)
	foundIndex := -1
	for i, line := range frontmatterLines {
		if noteIDLineRegex.MatchString(line) {
			foundIndex = i
			break
		}
	}

	if noteID == nil {
		if foundIndex == -1 {
			return markdown, nil
		}

		remaining := append(append([]string{}, frontmatterLines[:foundIndex]...), frontmatterLines[foundIndex+1:]...)

		// Remove the frontmatter entirely when noteId was its only content.
		if len(nonEmpty(remaining)) == 0 {
			body := lines[end+1:]
			if len(body) > 0 && strings.TrimSpace(body[0]) == "" {
				body = body[1:]
			}

			return strings.Join(body, "\n"), nil
		}

		result := append([]string{}, lines[:start+1]...)
		result = append(result, remaining...)
		result = append(result, lines[end:]...)
		return strings.Join(result, "\n"), nil
	}

	newLine := fmt.Sprintf("noteId: %d", *noteID)

	if foundIndex != -1 {
		frontmatterLines[foundIndex] = newLine
	} else {
		frontmatterLines = append(frontmatterLines, newLine)
	}

	result := append([]string{}, lines[:start+1]...)
	result = append(result, frontmatterLines...)
	result = append(result, lines[end:]...)
	return strings.Join(result, "\n"), nil
}

func nonEmpty(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}

	return result
}
