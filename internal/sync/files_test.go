package sync

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ihaaae/yanki/internal/config"
	"github.com/ihaaae/yanki/internal/vault"
)

// copyTree copies a directory tree into a destination directory.
func copyTree(t *testing.T, source, destination string) {
	t.Helper()

	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}

		target := filepath.Join(destination, relative)

		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, contents, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
}

func TestFilesSyncsVaultFixture(t *testing.T) {
	t.Parallel()

	fake := newFakeAnki()
	_, client := fake.server(t)

	root := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "testdata", "vault"), root)

	directory, err := vault.Resolve(root)
	if err != nil {
		t.Fatalf("resolve vault: %v", err)
	}

	notePaths := directory.FindNotePaths([]string{"Anki"}, true)
	if len(notePaths) != 4 {
		t.Fatalf("found %d notes, want 4", len(notePaths))
	}

	options := FileOptions{Namespace: "Test", StrictLineBreaks: true}

	first, err := Files(t.Context(), client, notePaths, options)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}

	if countActionInFiles(first, ActionCreated) != 4 {
		t.Fatalf("first sync actions = %v, want 4 created", first.Synced)
	}

	decks := map[string]bool{}
	for _, stored := range fake.notes {
		decks[stored.deck] = true
	}

	wantDecks := []string{"Animals", "Animals::Biped", "Animals::Quadruped", "Non-living things"}
	for _, want := range wantDecks {
		if !decks[want] {
			t.Errorf("deck %q was not created; have %v", want, decks)
		}
	}

	// Every synced file records its note ID in the frontmatter.
	for _, entry := range first.Synced {
		contents, err := os.ReadFile(entry.FilePath)
		if err != nil {
			t.Fatal(err)
		}

		want := "noteId: "
		if !strings.Contains(string(contents), want) {
			t.Errorf("file %s is missing a note ID:\n%s", entry.FilePath, contents)
		}
	}

	// A second sync is a no-op.
	second, err := Files(t.Context(), client, notePaths, options)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if countActionInFiles(second, ActionUnchanged) != 4 {
		t.Fatalf("second sync actions = %v, want 4 unchanged", second.Synced)
	}

	// Removing a file deletes its note.
	removed := notePaths[0]
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}

	third, err := Files(t.Context(), client, notePaths[1:], options)
	if err != nil {
		t.Fatalf("third sync: %v", err)
	}

	if countActionInFiles(third, ActionDeleted) != 1 {
		t.Fatalf("third sync actions = %v, want 1 deleted", third.Synced)
	}

	if len(fake.notes) != 3 {
		t.Errorf("expected 3 notes after deletion, got %d", len(fake.notes))
	}
}

func TestFilesDryRunLeavesFilesAlone(t *testing.T) {
	t.Parallel()

	fake := newFakeAnki()
	_, client := fake.server(t)

	root := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "testdata", "vault"), root)

	directory, err := vault.Resolve(root)
	if err != nil {
		t.Fatalf("resolve vault: %v", err)
	}

	notePaths := directory.FindNotePaths([]string{"Anki"}, true)
	original, err := os.ReadFile(notePaths[0])
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Files(t.Context(), client, notePaths, FileOptions{DryRun: true, Namespace: "Test", StrictLineBreaks: true}); err != nil {
		t.Fatalf("dry run: %v", err)
	}

	after, err := os.ReadFile(notePaths[0])
	if err != nil {
		t.Fatal(err)
	}

	if string(original) != string(after) {
		t.Errorf("dry run modified %s\nbefore: %s\nafter: %s", notePaths[0], original, after)
	}

	if len(fake.notes) != 0 {
		t.Errorf("dry run created %d notes, want 0", len(fake.notes))
	}
}

func countActionInFiles(result FilesResult, action Action) int {
	count := 0
	for _, entry := range result.Synced {
		if entry.Action == action {
			count++
		}
	}

	return count
}

func TestFilesPlacesNotesByPriority(t *testing.T) {
	t.Parallel()

	fake := newFakeAnki()
	_, client := fake.server(t)

	root := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "testdata", "vault"), root)

	// Frontmatter overrides the folder rule for this one note.
	horse := filepath.Join(root, "Anki", "Animals", "Quadruped", "Horse.md")
	contents, err := os.ReadFile(horse)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(horse, append([]byte("---\npriority: 3\n---\n"), contents...), 0o644); err != nil {
		t.Fatal(err)
	}

	directory, err := vault.Resolve(root)
	if err != nil {
		t.Fatalf("resolve vault: %v", err)
	}

	options := FileOptions{
		Namespace:        "Test",
		StrictLineBreaks: true,
		Root:             directory.RootPath,
		Priority: config.PrioritySettings{
			Decks:     []string{"A::P1", "A::P2", "A::P3"},
			Default:   2,
			Folders:   map[string]int{"Anki": 3, "Anki/Animals": 1},
			TagPrefix: "topic",
		},
	}

	if _, err := Files(t.Context(), client, directory.FindNotePaths(nil, true), options); err != nil {
		t.Fatalf("sync: %v", err)
	}

	type placement struct {
		deck string
		tags string
	}

	got := map[string]placement{}
	for _, stored := range fake.notes {
		got[stored.fields["Front"]] = placement{stored.deck, strings.Join(stored.tags, " ")}
	}

	want := map[string]placement{
		"Plato":   {"A::P1", "topic::Anki::Animals::Biped"},     // longest folder rule wins
		"Horse":   {"A::P3", "topic::Anki::Animals::Quadruped"}, // frontmatter beats folder
		"Rock":    {"A::P3", "topic::Anki::Non-living_things"},  // spaces are not allowed in tags
		"Outside": {"A::P2", ""},                                // root notes: default tier, no tag
	}

	for front, placed := range got {
		for word, expected := range want {
			if strings.Contains(front, word) && placed != expected {
				t.Errorf("note containing %q placed as %+v, want %+v", word, placed, expected)
			}
		}
	}

	if len(got) != 5 {
		t.Errorf("synced %d notes, want 5", len(got))
	}
}
