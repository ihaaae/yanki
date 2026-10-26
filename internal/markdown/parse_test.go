package markdown

import (
	"strings"
	"testing"

	"github.com/ihaaae/yanki/internal/note"
)

const testNamespace = "Yanki Test"

func parse(t *testing.T, markdown string) note.Note {
	t.Helper()

	parsed, err := Parse(markdown, Options{Namespace: testNamespace})
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", markdown, err)
	}

	return parsed
}

func TestInferModelAndFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		markdown  string
		model     note.ModelName
		front     []string
		back      []string
		extra     []string
		notInBack []string
	}{
		{
			name:     "basic",
			markdown: "This is the front of the card\n\n---\n\nThis is the back of the card",
			model:    note.ModelBasic,
			front:    []string{"<p>This is the front of the card</p>"},
			back:     []string{"<p>This is the back of the card</p>"},
		},
		{
			name:     "basic without a back",
			markdown: "Front only",
			model:    note.ModelBasic,
			front:    []string{"<p>Front only</p>"},
			back:     []string{},
		},
		{
			name:     "reversed with extra",
			markdown: "Sometimes the answer is the question\n\n---\n\n---\n\nSometimes the question is the answer\n\n---\n\nExtra content",
			model:    note.ModelBasicReversed,
			front:    []string{"<p>Sometimes the answer is the question</p>"},
			back:     []string{"<p>Sometimes the question is the answer</p>"},
			extra:    []string{"<p>Extra content</p>"},
		},
		{
			name:     "type in the answer",
			markdown: "Jazz isn't dead\n\n_It just smells funny_",
			model:    note.ModelTypeIn,
			front:    []string{"Jazz isn't dead"},
			back:     []string{"<p>It just smells funny</p>"},
		},
		{
			name:     "cloze",
			markdown: "All will be ~~revealed~~.",
			model:    note.ModelCloze,
			front:    []string{"All will be {{c1::revealed}}."},
		},
		{
			name:     "cloze with hint and back",
			markdown: "~~All~~ will be ~~revealed _here's a hint_~~.\n\n---\n\nAdditional revelations.",
			model:    note.ModelCloze,
			front:    []string{"{{c1::All}}", "{{c2::revealed::", "here's a hint", "}}"},
			back:     []string{"Additional revelations."},
		},
		{
			name:     "explicit cloze numbering",
			markdown: "~~1 All~~ will be ~~1 revealed~~.",
			model:    note.ModelCloze,
			front:    []string{"{{c1::All}}", "{{c1::revealed}}"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			parsed := parse(t, test.markdown)

			if parsed.ModelName != test.model {
				t.Fatalf("model = %q, want %q", parsed.ModelName, test.model)
			}

			for _, want := range test.front {
				if !strings.Contains(parsed.Fields.Front, want) {
					t.Errorf("front missing %q:\n%s", want, parsed.Fields.Front)
				}
			}

			for _, want := range test.back {
				if !strings.Contains(parsed.Fields.Back, want) {
					t.Errorf("back missing %q:\n%s", want, parsed.Fields.Back)
				}
			}

			for _, want := range test.extra {
				if !strings.Contains(parsed.Fields.Extra, want) {
					t.Errorf("extra missing %q:\n%s", want, parsed.Fields.Extra)
				}
			}

			for _, unwanted := range test.notInBack {
				if strings.Contains(parsed.Fields.Back, unwanted) {
					t.Errorf("back unexpectedly contains %q:\n%s", unwanted, parsed.Fields.Back)
				}
			}

			if parsed.Fields.YankiNamespace != testNamespace {
				t.Errorf("namespace = %q, want %q", parsed.Fields.YankiNamespace, testNamespace)
			}
		})
	}
}

func TestWrapperAndBoilerplate(t *testing.T) {
	t.Parallel()

	parsed := parse(t, "Front\n\n---\n\nBack")

	if !strings.HasPrefix(parsed.Fields.Front, boilerplateComment) {
		t.Errorf("front is missing the boilerplate comment:\n%s", parsed.Fields.Front)
	}

	if !strings.Contains(parsed.Fields.Front, `<div class="yanki namespace-yanki-test front model-yanki-basic">`) {
		t.Errorf("front wrapper classes are wrong:\n%s", parsed.Fields.Front)
	}

	if !strings.Contains(parsed.Fields.Back, `<div class="yanki namespace-yanki-test back model-yanki-basic">`) {
		t.Errorf("back wrapper classes are wrong:\n%s", parsed.Fields.Back)
	}
}

func TestEmptyFieldsUsePlaceholder(t *testing.T) {
	t.Parallel()

	parsed := parse(t, "Front\n\n---\n")

	if !strings.Contains(parsed.Fields.Back, "<p><em>(Empty)</em></p>") {
		t.Errorf("empty back should use the placeholder:\n%s", parsed.Fields.Back)
	}

	empty := parse(t, "")

	if !strings.Contains(empty.Fields.Front, "<p><em>(Empty)</em></p>") {
		t.Errorf("empty front should use the placeholder:\n%s", empty.Fields.Front)
	}

	if empty.Fields.Back != "" {
		t.Errorf("empty document back should be empty, got:\n%s", empty.Fields.Back)
	}
}

func TestFrontmatterTagsAndNoteID(t *testing.T) {
	t.Parallel()

	parsed := parse(t, "---\nnoteId: 42\ntags:\n  - parent/child\n  - single\n---\n\nFront\n\n---\n\nBack")

	if parsed.NoteID != 42 {
		t.Errorf("note ID = %d, want 42", parsed.NoteID)
	}

	if len(parsed.Tags) != 2 || parsed.Tags[0] != "parent::child" {
		t.Errorf("tags = %v, want [parent::child single]", parsed.Tags)
	}

	if !strings.Contains(parsed.Fields.Front, "<p>Front</p>") {
		t.Errorf("frontmatter leaked into the front field:\n%s", parsed.Fields.Front)
	}
}

func TestMathUsesAnkiDelimiters(t *testing.T) {
	t.Parallel()

	parsed := parse(t, "Inline $a_1 * b_2 < c$ and ~~$x^2$~~, the $y$**-axis**\n\n$$\n\\sum_i x_i\n$$\n\n---\n\nAWK prints `$1` and $1 and $2, costs $5 or \\$6")

	for _, want := range []string{`\(a_1 * b_2 &lt; c\)`, `{{c1::\(x^2\)}}`, `\(y\)<strong>-axis</strong>`, `\[\sum_i x_i\]`} {
		if !strings.Contains(parsed.Fields.Front, want) {
			t.Errorf("front is missing %q:\n%s", want, parsed.Fields.Front)
		}
	}

	if want := "<code>$1</code> and $1 and $2, costs $5 or $6"; !strings.Contains(parsed.Fields.Back, want) {
		t.Errorf("back should keep dollars that are not math, want %q:\n%s", want, parsed.Fields.Back)
	}
}
