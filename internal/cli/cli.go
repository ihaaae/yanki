// Package cli implements the Yanki command line interface.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ihaaae/yanki/internal/anki"
	"github.com/ihaaae/yanki/internal/config"
	"github.com/ihaaae/yanki/internal/sync"
	"github.com/ihaaae/yanki/internal/vault"
	"github.com/spf13/cobra"
)

// Version is the reported CLI version. It is overridden at build time.
var Version = "dev"

// runtime bundles everything a command needs after flag and config resolution.
type runtime struct {
	directory       vault.Directory
	settings        config.Settings
	namespace       string
	namespaceSource config.NamespaceSource
	configPath      string
	notePaths       []string
}

// flags holds every command line value before resolution.
type flags struct {
	configPath        string
	folders           []string
	namespace         string
	ankiConnect       string
	ankiKey           string
	ankiWeb           bool
	syncMedia         string
	strictLineBreaks  bool
	ignoreFolderNotes bool
	json              bool
	verbose           bool
	dryRun            bool
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	command := newRootCommand()

	if err := command.Execute(); err != nil {
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}

		return 1
	}

	return 0
}

func newRootCommand() *cobra.Command {
	values := &flags{}

	root := &cobra.Command{
		Use:           "yanki [directory]",
		Short:         "Sync Markdown notes to Anki through AnkiConnect",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runSync(command, args, values)
		},
	}

	addSharedFlags(root, values)
	root.Version = Version

	root.AddCommand(
		&cobra.Command{
			Use:   "sync [directory]",
			Short: "Sync Markdown notes to Anki (the default command)",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runSync(command, args, values)
			},
		},
		&cobra.Command{
			Use:   "config [directory]",
			Short: "Print the settings a sync would use",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runConfig(command, args, values)
			},
		},
		&cobra.Command{
			Use:   "list [directory]",
			Short: "List the notes Yanki manages in Anki",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runList(command, args, values)
			},
		},
		&cobra.Command{
			Use:   "clean [directory]",
			Short: "Delete every note Yanki manages in Anki for the namespace",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runClean(command, args, values)
			},
		},
	)

	return root
}

func addSharedFlags(command *cobra.Command, values *flags) {
	persistent := command.PersistentFlags()

	persistent.StringVar(&values.configPath, "config", "", "path to a TOML settings file")
	persistent.StringArrayVar(&values.folders, "folder", nil, "flashcard folder, relative to the synced directory (repeatable; overrides the config file)")
	persistent.StringVar(&values.namespace, "namespace", "", "namespace used to recognize Yanki's notes in Anki")
	persistent.StringVar(&values.ankiConnect, "anki-connect", "", "AnkiConnect URL, e.g. http://127.0.0.1:8765")
	persistent.StringVar(&values.ankiKey, "anki-key", "", "AnkiConnect API key, if required")
	persistent.BoolVar(&values.ankiWeb, "anki-web", false, "push to AnkiWeb after syncing locally")
	persistent.StringVar(&values.syncMedia, "sync-media", "", "media sync mode: off, local, remote, or all")
	persistent.BoolVar(&values.strictLineBreaks, "strict-line-breaks", false, "treat single newlines in Markdown as line breaks")
	persistent.BoolVar(&values.ignoreFolderNotes, "ignore-folder-notes", false, "skip notes that share their parent folder's name")
	persistent.BoolVar(&values.json, "json", false, "print the result as JSON")
	persistent.BoolVarP(&values.verbose, "verbose", "v", false, "print additional details")
	persistent.BoolVarP(&values.dryRun, "dry-run", "d", false, "show what would change without modifying Anki")
}

// prepare resolves the directory, settings, namespace, and note selection
// shared by every command.
func prepare(command *cobra.Command, values *flags, args []string) (runtime, error) {
	directoryPath := "."
	if len(args) > 0 {
		directoryPath = args[0]
	}

	directory, err := vault.Resolve(directoryPath)
	if err != nil {
		return runtime{}, err
	}

	settings, configPath, err := config.Load(directory.RootPath, values.configPath)
	if err != nil {
		return runtime{}, err
	}

	// Only flags the user actually set override the settings, so a flag whose
	// default differs from the setting can still be turned off explicitly.
	changed := command.Flags()
	overrides := config.Overrides{}

	if changed.Changed("folder") {
		overrides.Folders = values.folders
	}

	if changed.Changed("namespace") {
		overrides.Namespace = &values.namespace
	}

	if changed.Changed("anki-connect") {
		overrides.AnkiConnectURL = &values.ankiConnect
	}

	if changed.Changed("anki-key") {
		overrides.AnkiKey = &values.ankiKey
	}

	if changed.Changed("anki-web") {
		overrides.AnkiWeb = &values.ankiWeb
	}

	if changed.Changed("sync-media") {
		overrides.SyncMedia = &values.syncMedia
	}

	if changed.Changed("strict-line-breaks") {
		overrides.StrictLineBreaks = &values.strictLineBreaks
	}

	if changed.Changed("ignore-folder-notes") {
		overrides.IgnoreFolderNotes = &values.ignoreFolderNotes
	}

	if err := settings.ApplyOverrides(overrides); err != nil {
		return runtime{}, err
	}

	var explicitNamespace *string
	if changed.Changed("namespace") {
		explicitNamespace = &values.namespace
	}

	namespace, source, err := config.ResolveNamespace(explicitNamespace, settings, configPath, directory.Name)
	if err != nil {
		return runtime{}, err
	}

	settings.Namespace = namespace

	return runtime{
		configPath:      configPath,
		directory:       directory,
		namespace:       namespace,
		namespaceSource: source,
		notePaths:       directory.FindNotePaths(settings.Folders, settings.IgnoreFolderNotes),
		settings:        settings,
	}, nil
}

func (r runtime) client() *anki.Client {
	return anki.NewClient(r.settings.URL(), r.settings.AnkiConnect.Key)
}

func runSync(command *cobra.Command, args []string, values *flags) error {
	state, err := prepare(command, values, args)
	if err != nil {
		return err
	}

	if len(state.notePaths) == 0 {
		return fmt.Errorf("no Markdown notes found in %s", folderDescription(state))
	}

	if state.settings.Sync.MediaMode != "off" {
		fmt.Fprintln(command.ErrOrStderr(), "Warning: media asset sync is not implemented yet; embedded media will not be synced to Anki.")
	}

	if values.verbose && !values.json {
		fmt.Fprintln(command.OutOrStdout(), formatContext(state, len(state.notePaths)))
	}

	result, err := sync.Files(command.Context(), state.client(), state.notePaths, sync.FileOptions{
		CheckDatabase:    true,
		DryRun:           values.dryRun,
		Namespace:        state.namespace,
		PushToAnkiWeb:    state.settings.Sync.PushToAnkiWeb,
		StrictLineBreaks: state.settings.StrictLineBreaks,
		Root:             state.directory.RootPath,
		Priority:         state.settings.Priority,
	})
	if err != nil {
		return err
	}

	if values.json {
		return writeJSON(command, result)
	}

	unreachable := false
	for _, entry := range result.Synced {
		if entry.Action == sync.ActionAnkiUnreachable {
			unreachable = true
			break
		}
	}

	if unreachable {
		fmt.Fprintln(command.ErrOrStderr(), "Could not connect to Anki. Make sure the Anki desktop application is running, and that the AnkiConnect add-on is installed.")
		return errSilentFailure
	}

	fmt.Fprintln(command.OutOrStdout(), formatSyncResult(result, values.verbose))

	return nil
}

func runConfig(command *cobra.Command, args []string, values *flags) error {
	state, err := prepare(command, values, args)
	if err != nil {
		return err
	}

	if values.json {
		return writeJSON(command, map[string]any{
			"ankiConnect":       state.settings.URL(),
			"configFile":        state.configPath,
			"directory":         state.directory.RootPath,
			"folders":           state.settings.Folders,
			"ignoreFolderNotes": state.settings.IgnoreFolderNotes,
			"namespace":         state.namespace,
			"namespaceSource":   string(state.namespaceSource),
			"notes":             len(state.notePaths),
			"pushToAnkiWeb":     state.settings.Sync.PushToAnkiWeb,
			"strictLineBreaks":  state.settings.StrictLineBreaks,
			"syncMedia":         state.settings.Sync.MediaMode,
		})
	}

	fmt.Fprintln(command.OutOrStdout(), formatContext(state, len(state.notePaths)))

	return nil
}

func runList(command *cobra.Command, args []string, values *flags) error {
	state, err := prepare(command, values, args)
	if err != nil {
		return err
	}

	notes, err := sync.ListRemote(command.Context(), state.client(), state.namespace)
	if err != nil {
		return err
	}

	if values.json {
		return writeJSON(command, notes)
	}

	if len(notes) == 0 {
		fmt.Fprintf(command.OutOrStdout(), "No notes found for namespace %q.\n", state.namespace)
		return nil
	}

	for _, listed := range notes {
		fmt.Fprintf(command.OutOrStdout(), "%d\t%s\t%s\n", listed.NoteID, listed.DeckName, listed.ModelName)
	}

	return nil
}

func runClean(command *cobra.Command, args []string, values *flags) error {
	state, err := prepare(command, values, args)
	if err != nil {
		return err
	}

	deleted, err := sync.CleanNamespace(command.Context(), state.client(), state.namespace, values.dryRun)
	if err != nil {
		return err
	}

	if values.json {
		return writeJSON(command, map[string]any{"deleted": len(deleted), "dryRun": values.dryRun})
	}

	verb := "Deleted"
	if values.dryRun {
		verb = "Would delete"
	}

	fmt.Fprintf(command.OutOrStdout(), "%s %d notes for namespace %q.\n", verb, len(deleted), state.namespace)

	return nil
}

// errSilentFailure reports a failure without printing a second message.
var errSilentFailure = errors.New("")

// writeJSON prints a value as indented JSON.
func writeJSON(command *cobra.Command, value any) error {
	encoder := json.NewEncoder(command.OutOrStdout())
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}

func folderDescription(state runtime) string {
	if len(state.settings.Folders) == 0 {
		return state.directory.RootPath
	}

	return strings.Join(state.settings.Folders, ", ")
}

// formatContext describes the settings a command will act on.
func formatContext(state runtime, noteCount int) string {
	lines := []string{
		"Directory: " + state.directory.RootPath,
	}

	if state.configPath != "" {
		lines = append(lines, "Config: "+state.configPath)
	}

	lines = append(lines,
		fmt.Sprintf("Namespace: %s (from %s)", state.namespace, state.namespaceSource),
		"Flashcard folders: "+folderDescription(state),
		fmt.Sprintf("Notes to sync: %d", noteCount),
		"AnkiConnect: "+state.settings.URL(),
		"Sync media assets: "+state.settings.Sync.MediaMode,
		fmt.Sprintf("Push to AnkiWeb: %s", yesNo(state.settings.Sync.PushToAnkiWeb)),
		fmt.Sprintf("Strict line breaks: %s", yesNo(state.settings.StrictLineBreaks)),
		fmt.Sprintf("Ignore folder notes: %s", yesNo(state.settings.IgnoreFolderNotes)),
	)

	return strings.Join(lines, "\n")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}
