// Package note holds Yanki's note models and the note representation shared
// between Markdown parsing and the AnkiConnect sync.
package note

import "github.com/ihaaae/yanki/internal/anki"

// ModelName identifies one of the note types Yanki manages in Anki.
type ModelName string

const (
	// ModelBasic is a simple front/back card.
	ModelBasic ModelName = "Yanki - Basic"
	// ModelCloze is a cloze-deletion card.
	ModelCloze ModelName = "Yanki - Cloze"
	// ModelTypeIn is a card where the answer must be typed.
	ModelTypeIn ModelName = "Yanki - Basic (type in the answer)"
	// ModelBasicReversed is a front/back card with an extra field, generating a
	// reversed card as well.
	ModelBasicReversed ModelName = "Yanki - Basic (and reversed card with extra)"
)

// LegacyModelBasicReversed is a model name Yanki used to create. It is still
// recognized when reading existing notes so old collections keep working, but
// no new notes use it.
const LegacyModelBasicReversed ModelName = "Yanki - Basic (and reversed card)"

// ModelNames lists the models Yanki creates and manages, in a stable order.
var ModelNames = []ModelName{ModelBasic, ModelCloze, ModelTypeIn, ModelBasicReversed}

// KnownModelNames includes the legacy name, for validating notes read from
// Anki.
var KnownModelNames = append([]ModelName{LegacyModelBasicReversed}, ModelNames...)

// DefaultCSS matches Anki's stock card styling.
const DefaultCSS = `.card {
	font-family: arial;
	font-size: 20px;
	line-height: 1.5;
	text-align: center;
	color: black;
	background-color: white;
}

` + tagsCSS

// tagsCSS styles the tag header that every Yanki card face starts with.
const tagsCSS = `.yanki-tags {
	font-size: 0.6em;
	color: gray;
	margin-bottom: 0.5em;
}
`

// DefaultDeckName is the deck used when a note's file path yields no deck.
const DefaultDeckName = "Yanki"

// NamespaceMaxLength bounds a namespace, keeping it typeable and out of the way
// in media filenames.
const NamespaceMaxLength = 60

// DefaultEmptyText stands in for content that is required but missing, so that
// every Markdown file maps to a note.
const DefaultEmptyText = "(Empty)"

// CSSDefaultClassName is the class on the wrapper div around every rendered
// field, available for custom styling.
const CSSDefaultClassName = "yanki"

// tagsHeader shows the note's tags above every card face, so a terse card
// such as an operator cloze still says which subject it belongs to.
const tagsHeader = `<div class="yanki-tags">{{Tags}}</div>\n`

// Models returns the createModel definitions for every model Yanki manages.
func Models() []anki.Model {
	return []anki.Model{
		{
			CardTemplates: []anki.CardTemplate{
				{Qfmt: tagsHeader + "{{Front}}", Afmt: "{{FrontSide}}\n\n<hr id=answer>\n\n{{Back}}"},
			},
			InOrderFields: []string{"Front", "Back", "YankiNamespace"},
			CSS:           DefaultCSS,
			ModelName:     string(ModelBasic),
		},
		{
			CardTemplates: []anki.CardTemplate{
				{Qfmt: tagsHeader + "{{cloze:Front}}", Afmt: tagsHeader + "{{cloze:Front}}<br>\n{{Back}}"},
			},
			InOrderFields: []string{"Front", "Back", "YankiNamespace"},
			IsCloze:       true,
			CSS:           DefaultCSS,
			ModelName:     string(ModelCloze),
		},
		{
			CardTemplates: []anki.CardTemplate{
				{Qfmt: tagsHeader + "{{Front}}\n\n{{type:Back}}", Afmt: tagsHeader + "{{Front}}\n\n<hr id=answer>\n\n{{type:Back}}"},
			},
			InOrderFields: []string{"Front", "Back", "YankiNamespace"},
			CSS:           DefaultCSS,
			ModelName:     string(ModelTypeIn),
		},
		{
			CardTemplates: []anki.CardTemplate{
				{
					Qfmt: tagsHeader + "{{Front}}",
					Afmt: "{{FrontSide}}\n\n<hr id=answer>\n\n{{Back}}{{#Extra}}\n\n<hr>\n\n{{Extra}}{{/Extra}}",
				},
				{
					Qfmt: tagsHeader + "{{Back}}",
					Afmt: "{{FrontSide}}\n\n<hr id=answer>\n\n{{Front}}{{#Extra}}\n\n<hr>\n\n{{Extra}}{{/Extra}}",
				},
			},
			InOrderFields: []string{"Front", "Back", "Extra", "YankiNamespace"},
			CSS:           DefaultCSS,
			ModelName:     string(ModelBasicReversed),
		},
	}
}

// ModelDefinition returns the createModel definition for a model name, and
// whether it is one Yanki manages.
func ModelDefinition(name ModelName) (anki.Model, bool) {
	for _, definition := range Models() {
		if definition.ModelName == string(name) {
			return definition, true
		}
	}

	return anki.Model{}, false
}
