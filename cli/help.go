package cli

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"
)

//go:embed markdownStyles.json
var markdownStyles []byte

var helpPrinter func(w io.Writer, templ string, data any)

func init() {
	helpPrinter = cli.HelpPrinter
	cli.HelpPrinter = HelpPrinter
}

var helpTemplateMarkdown = `Usage: {{ template "usageTemplate" . }}{{ if .Description }}

{{ description .Description }}{{ end }}{{ if .VisibleCommands }}

{{ commands .VisibleCommands }}{{ end }}{{ if .VisibleFlags }}

{{ flags .VisibleFlags }}{{ end }}
Run "{{ .App.Name }} <command> --help" for more information on a command.
`

// HelpPrinter is a custom printer for OpenAPI commands.
// The base docs URL can be overridden by setting the OKAPI_DOCS_URL environment variable.
func HelpPrinter(w io.Writer, templ string, data any) {
	templ = helpTemplateMarkdown
	theme := "notty"
	width := 80

	// Try to do a little detection of the writer to use the right template and colors
	if f, ok := w.(*os.File); ok {
		output := termenv.NewOutput(f)
		if output.Profile != termenv.Ascii {
			theme = "dracula"
		}
		width, _, _ = term.GetSize(int(f.Fd()))
	}

	// Reference path URLs against docs — allow override via env
	baseUrl := os.Getenv("OKAPI_DOCS_URL")

	// Create our markdown renderer
	markdown, _ := glamour.NewTermRenderer(
		glamour.WithBaseURL(baseUrl),
		glamour.WithStandardStyle(theme),
		glamour.WithStylesFromJSONBytes(markdownStyles),
		glamour.WithWordWrap(width),
	)

	// Use the default printer to leverage the subtemplates
	buf := new(strings.Builder)

	customFuncs := map[string]any{
		"description": func(s string) string {
			// TODO(shakefu): Figure out if we really need to double-trim here
			s = strings.TrimSpace(s)
			s, _ = markdown.Render(s)
			return strings.TrimSpace(s)
		},
		"commands": func(commands []*cli.Command) string {
			// Create a buf for output
			s := new(strings.Builder)
			mapped := MapCommands(commands)
			ListPrint(s, "Commands", mapped, 2, 4, width)
			return strings.TrimSpace(s.String())
		},
		"flags": func(flags []cli.Flag) string {
			// Create a buf for output
			s := new(strings.Builder)
			mapped := MapFlags(flags)
			ListPrint(s, "Flags", mapped, 2, 4, width)
			return strings.TrimSpace(s.String())
		},
	}

	// helpPrinter(buf, templ, data)
	cli.HelpPrinterCustom(buf, templ, data, customFuncs)

	// Write the result to whatever output we have
	w.Write([]byte(buf.String()))
}

// MapCommands creates a map of command names to usage string, which is used in
// the help printer
func MapCommands(commands []*cli.Command) map[string]string {
	data := make(map[string]string)

	for _, command := range commands {
		data[command.Name] = command.Usage
	}

	return data
}

// FlagName returns flag with prepended hyphens, and joins multiple flag names
func FlagName(flag cli.Flag) string {
	flagNames := flag.Names()
	for i := range flagNames {
		if flagNames[i] == "" {
			continue
		}
		if len(flagNames[i]) == 1 {
			flagNames[i] = "-" + flagNames[i]
		} else {
			flagNames[i] = "--" + flagNames[i]
		}
	}
	return strings.Join(flagNames, ", ")
}

// MapFlags creates a map of flag names to usage string, which is used in the
// help printer
func MapFlags(flags []cli.Flag) map[string]string {
	data := make(map[string]string)

	for _, flag := range flags {
		name := FlagName(flag)

		docFlag, ok := flag.(cli.DocGenerationFlag)
		if !ok {
			// Fuck it
			continue
		}
		usage := docFlag.GetUsage()
		usage = strings.Replace(usage, "\n", " ", -1)

		data[name] = usage
	}

	return data
}

// ListPrint is used to print lists of commands and flags
func ListPrint(w io.Writer, category string, data map[string]string, indent int, offset int, width int) {
	// Calculate width of the longest name
	maxLen := 0
	for name := range data {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}
	// Blank indentation so all usage wrapped strings are aligned
	filler := strings.Repeat(` `, indent+maxLen+offset)
	wrapped := make(map[string]string)

	// Calculate wrapping of the values
	wrapAt := width - maxLen - offset - indent
	for name, usage := range data {
		// Splitting words on spaces for wrapping
		words := strings.Split(usage, " ")
		lines := make([]string, 0, 16)
		line := ""
		for _, word := range words {
			if len(line)+len(word) >= wrapAt {
				// Save the wrapped line
				lines = append(lines, line)
				// Keep going
				line = ""
			} else {
				line += " "
			}
			line += word
		}
		if len(line) > 0 {
			lines = append(lines, line)
		}
		if len(lines) < 1 {
			// This shouldn't happen?
			wrapped[name] = usage + "\n" // probably an empty string
			continue
		}
		// First line is handled explicitly so we can prefix with the name
		entry := filler[:indent]
		entry += name
		entry += filler[:maxLen+offset-len(name)-1]
		entry += lines[0]
		entry += "\n"
		// Remaining lines are prepended with the filler
		for i := 1; i < len(lines); i++ {
			entry += filler
			entry += lines[i]
			entry += "\n"
		}
		wrapped[name] = entry
	}

	// Defaults that get sorted to the back of the list
	defaultNames := map[string]bool{
		"debug":   true,
		"help":    true,
		"verbose": true,
		"version": true,
	}
	defaults := []string{"debug", "help", "verbose", "version"}

	// Get the list of all names so we can print in alphabetical order
	names := make([]string, 0, len(wrapped))
	sorted := make(map[string]string, len(wrapped))
	for name := range wrapped {
		// Get all the possible sorting names
		allNames := strings.Split(name, ",")
		for i := range allNames {
			allNames[i] = strings.ToLower(strings.Trim(allNames[i], "-, "))
			allNames[i] = strings.Split(allNames[i], "=")[0]
		}

		// Get the longest name
		sort.Slice(allNames, func(i, j int) bool {
			l1, l2 := len(allNames[i]), len(allNames[j])
			if l1 != l2 {
				return l1 > l2
			}
			return allNames[i] > allNames[j]
		})

		// The longest name is the one we want to sort by
		sortingName := allNames[0]
		sorted[sortingName] = name
		// Skip defaults in the list to sort, so we can append later
		if defaultNames[sortingName] {
			continue
		}

		names = append(names, sortingName)
	}
	sort.Strings(names)

	// Shove the defaults onto the back of the list, they'll get skipped if they
	// aren't real
	names = append(names, defaults...)

	// Print the category
	fmt.Fprint(w, category+":\n")

	// Print the wrapped lines
	for _, sortingName := range names {
		name, ok := sorted[sortingName]
		if !ok {
			continue
		}
		fmt.Fprint(w, wrapped[name])
	}
}
