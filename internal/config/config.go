// Package config resolves the settings a sync acts on: built-in defaults, an
// optional TOML file, and command-line overrides.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/ihaaae/yanki/internal/note"
)

// ConfigFileName is the file Yanki looks for inside the synced directory.
const ConfigFileName = "yanki.toml"

// AnkiConnectSettings locates the AnkiConnect server.
type AnkiConnectSettings struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
	Key  string `toml:"key"`
}

// SyncSettings controls what a sync does beyond writing notes.
type SyncSettings struct {
	// MediaMode is one of "off", "local", "remote", or "all".
	MediaMode string `toml:"media_mode"`
	// PushToAnkiWeb triggers an AnkiWeb sync after a local sync.
	PushToAnkiWeb bool `toml:"push_to_anki_web"`
}

// PrioritySettings files notes into one flat deck per priority tier instead of
// a deck per folder. The folder hierarchy is kept as hierarchical tags.
type PrioritySettings struct {
	// Decks lists the deck for each tier, highest priority first. Priority
	// placement is off when it is empty.
	Decks []string `toml:"decks"`
	// Default is the 1-based tier for notes no folder rule or frontmatter
	// `priority` covers.
	Default int `toml:"default"`
	// Folders maps a folder path relative to the synced directory, such as
	// "Math" or "Eng/Words", to a tier. The longest matching folder wins.
	Folders map[string]int `toml:"folders"`
	// TagPrefix prefixes the folder tag, e.g. "topic" gives
	// "topic::Math::4-Stat". Notes directly in the root get no folder tag.
	TagPrefix string `toml:"tag_prefix"`
}

// Enabled reports whether notes are placed by priority.
func (p PrioritySettings) Enabled() bool {
	return len(p.Decks) > 0
}

// validate checks that every tier refers to a configured deck.
func (p PrioritySettings) validate() error {
	if !p.Enabled() {
		return nil
	}

	if p.Default < 1 || p.Default > len(p.Decks) {
		return fmt.Errorf("priority.default must be between 1 and %d", len(p.Decks))
	}

	for folder, tier := range p.Folders {
		if tier < 1 || tier > len(p.Decks) {
			return fmt.Errorf("priority.folders.%q must be between 1 and %d", folder, len(p.Decks))
		}
	}

	return nil
}

// Settings is the full resolved configuration.
type Settings struct {
	AnkiConnect       AnkiConnectSettings `toml:"anki_connect"`
	Folders           []string            `toml:"folders"`
	IgnoreFolderNotes bool                `toml:"ignore_folder_notes"`
	Namespace         string              `toml:"namespace"`
	StrictLineBreaks  bool                `toml:"strict_line_breaks"`
	Sync              SyncSettings        `toml:"sync"`
	Priority          PrioritySettings    `toml:"priority"`
}

// Defaults returns the built-in settings.
func Defaults() Settings {
	return Settings{
		AnkiConnect: AnkiConnectSettings{
			Host: "http://127.0.0.1",
			Port: 8765,
		},
		IgnoreFolderNotes: true,
		Namespace:         "Yanki",
		StrictLineBreaks:  true,
		Sync: SyncSettings{
			// Media sync is not implemented yet, so the default is off. This
			// will become "local" when media assets are supported.
			MediaMode:     "off",
			PushToAnkiWeb: true,
		},
	}
}

// URL returns the AnkiConnect base URL.
func (s Settings) URL() string {
	return fmt.Sprintf("%s:%d", strings.TrimRight(s.AnkiConnect.Host, "/"), s.AnkiConnect.Port)
}

// Load reads settings from explicitPath when set, otherwise from the
// config file inside directory when it exists. It returns the loaded settings
// (merged over the defaults) and the path that was read, if any.
func Load(directory, explicitPath string) (Settings, string, error) {
	path := explicitPath
	if path == "" {
		candidate := filepath.Join(directory, ConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		}
	}

	if path == "" {
		return Defaults(), "", nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, "", fmt.Errorf("read config %s: %w", path, err)
	}

	settings := Defaults()
	metadata, err := toml.Decode(string(contents), &settings)
	if err != nil {
		return Settings{}, "", fmt.Errorf("parse config %s: %w", path, err)
	}

	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}

		return Settings{}, "", fmt.Errorf("unknown config keys in %s: %s", path, strings.Join(keys, ", "))
	}

	if err := settings.Priority.validate(); err != nil {
		return Settings{}, "", fmt.Errorf("config %s: %w", path, err)
	}

	return settings, path, nil
}

// Overrides carries command-line values. Nil fields leave the underlying
// setting untouched.
type Overrides struct {
	Namespace         *string
	Folders           []string
	AnkiConnectURL    *string
	AnkiKey           *string
	AnkiWeb           *bool
	SyncMedia         *string
	StrictLineBreaks  *bool
	IgnoreFolderNotes *bool
}

// ApplyOverrides applies command-line values on top of the settings.
func (s *Settings) ApplyOverrides(overrides Overrides) error {
	if len(overrides.Folders) > 0 {
		s.Folders = overrides.Folders
	}

	if overrides.IgnoreFolderNotes != nil {
		s.IgnoreFolderNotes = *overrides.IgnoreFolderNotes
	}

	if overrides.AnkiConnectURL != nil {
		host, port, err := ParseHostAndPort(*overrides.AnkiConnectURL)
		if err != nil {
			return err
		}

		s.AnkiConnect.Host = host
		s.AnkiConnect.Port = port
	}

	if overrides.AnkiKey != nil {
		s.AnkiConnect.Key = *overrides.AnkiKey
	}

	if overrides.AnkiWeb != nil {
		s.Sync.PushToAnkiWeb = *overrides.AnkiWeb
	}

	if overrides.SyncMedia != nil {
		switch *overrides.SyncMedia {
		case "off", "local", "remote", "all":
			s.Sync.MediaMode = *overrides.SyncMedia
		default:
			return fmt.Errorf("invalid sync media mode %q", *overrides.SyncMedia)
		}
	}

	if overrides.StrictLineBreaks != nil {
		s.StrictLineBreaks = *overrides.StrictLineBreaks
	}

	return nil
}

// NamespaceSource describes where the effective namespace came from.
type NamespaceSource string

const (
	// NamespaceFromOption means the --namespace flag set it.
	NamespaceFromOption NamespaceSource = "the --namespace option"
	// NamespaceFromConfig means the config file set it.
	NamespaceFromConfig NamespaceSource = "the config file"
	// NamespaceFromDirectory means it was derived from the directory name.
	NamespaceFromDirectory NamespaceSource = "the directory name"
)

// ResolveNamespace picks the namespace, preferring explicit choices over a
// directory-derived default, and validates it.
func ResolveNamespace(explicit *string, settings Settings, configPath, directoryName string) (string, NamespaceSource, error) {
	var (
		namespace string
		source    NamespaceSource
	)

	switch {
	case explicit != nil:
		namespace, source = *explicit, NamespaceFromOption
	case configPath != "":
		namespace, source = settings.Namespace, NamespaceFromConfig
	default:
		namespace, source = fmt.Sprintf("Yanki - %s", directoryName), NamespaceFromDirectory
	}

	sanitized, err := note.ValidateAndSanitizeNamespace(namespace)
	if err != nil {
		return "", "", err
	}

	return sanitized, source, nil
}

// ParseHostAndPort parses an AnkiConnect URL such as http://127.0.0.1:8765
// into a host and port.
func ParseHostAndPort(rawURL string) (string, int, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", 0, errors.New("empty AnkiConnect URL")
	}

	scheme := "http://"
	rest := trimmed
	if index := strings.Index(trimmed, "://"); index != -1 {
		scheme = trimmed[:index+3]
		rest = trimmed[index+3:]
	}

	host := rest
	port := 0

	if index := strings.LastIndex(rest, ":"); index != -1 {
		host = rest[:index]
		if _, err := fmt.Sscanf(rest[index+1:], "%d", &port); err != nil {
			return "", 0, fmt.Errorf("invalid AnkiConnect port in %q", rawURL)
		}
	} else {
		return "", 0, fmt.Errorf("AnkiConnect URL %q is missing a port", rawURL)
	}

	if host == "" {
		return "", 0, fmt.Errorf("AnkiConnect URL %q is missing a host", rawURL)
	}

	return scheme + host, port, nil
}
