package qa

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"transcription-cli/internal/logging"
	"golang.org/x/term"
)

// ANSI codes for interactive display.
const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiCyan  = "\033[36m"
)

// InteractiveQA prompts the user to review each file using arrow-key
// selection and writes the results back to the checklist JSON.
// Falls back to the manual WaitForQA flow when stdin is not a terminal.
func InteractiveQA(checklistPath, transcribedDir string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		WaitForQA(checklistPath, transcribedDir)
		return nil
	}

	data, err := os.ReadFile(checklistPath)
	if err != nil {
		return fmt.Errorf("reading checklist: %w", err)
	}
	var checklist Checklist
	if err := json.Unmarshal(data, &checklist); err != nil {
		return fmt.Errorf("parsing checklist: %w", err)
	}

	if len(checklist.Files) == 0 {
		logging.StepWarn("No files to review")
		return nil
	}

	logging.Banner("QA REVIEW")
	logging.StepInfo(fmt.Sprintf("Files: %s", transcribedDir))
	logging.StepInfo("Use \u2191/\u2193 arrows to select, Enter to confirm")
	fmt.Fprintln(os.Stderr)

	labels := []string{"\u2713 Approve", "\u2717 Rework"}

	for i := range checklist.Files {
		item := &checklist.Files[i]

		// Default to current status if already reviewed (e.g. resume).
		defaultIdx := 0
		if item.Status == "rework" {
			defaultIdx = 1
		}

		langInfo := ""
		if item.Language != "" {
			langInfo = fmt.Sprintf(" %s(%s)%s", ansiDim, item.Language, ansiReset)
		}
		fmt.Fprintf(os.Stderr, "  %s[%d/%d]%s %s%s\n",
			ansiBold+ansiCyan, i+1, len(checklist.Files), ansiReset,
			item.Name, langInfo)
		fmt.Fprintln(os.Stderr)

		choice := selectOption(labels, defaultIdx)

		if choice == 0 {
			item.Status = "approved"
			item.Notes = ""
			fmt.Fprintf(os.Stderr, "       %s\u2713 Approved%s\n\n", ansiGreen, ansiReset)
		} else {
			item.Status = "rework"
			fmt.Fprintf(os.Stderr, "       %s\u2717 Rework%s\n", ansiRed, ansiReset)
			fmt.Fprintf(os.Stderr, "       Notes: ")
			reader := bufio.NewReader(os.Stdin)
			notes, _ := reader.ReadString('\n')
			item.Notes = strings.TrimSpace(notes)
			fmt.Fprintln(os.Stderr)
		}
	}

	// Atomic write: tmp file + rename.
	out, err := json.MarshalIndent(checklist, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling checklist: %w", err)
	}
	tmp := checklistPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return fmt.Errorf("writing checklist tmp: %w", err)
	}
	if err := os.Rename(tmp, checklistPath); err != nil {
		return fmt.Errorf("renaming checklist: %w", err)
	}

	return nil
}

// selectOption presents a list of options with an arrow-key selector
// and returns the index of the chosen option.
func selectOption(labels []string, defaultIdx int) int {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return defaultIdx
	}
	defer term.Restore(fd, oldState)

	selected := defaultIdx
	buf := make([]byte, 3)

	render := func() {
		for i, label := range labels {
			if i == selected {
				fmt.Fprintf(os.Stderr, "\r\033[K    %s\u276f %s%s\r\n", ansiCyan, label, ansiReset)
			} else {
				fmt.Fprintf(os.Stderr, "\r\033[K    %s  %s%s\r\n", ansiDim, label, ansiReset)
			}
		}
		// Move cursor back to the first option line.
		fmt.Fprintf(os.Stderr, "\033[%dA", len(labels))
	}

	render()

	for {
		n, err := os.Stdin.Read(buf[:])
		if err != nil {
			break
		}

		if n == 1 {
			switch buf[0] {
			case 13, 10: // Enter
				// Clear the option lines as we move past them.
				for range labels {
					fmt.Fprintf(os.Stderr, "\r\033[K\r\n")
				}
				return selected
			case 3: // Ctrl-C
				term.Restore(fd, oldState)
				fmt.Fprintf(os.Stderr, "\r\n")
				os.Exit(1)
			case 'k': // vim-style up
				if selected > 0 {
					selected--
				}
			case 'j': // vim-style down
				if selected < len(labels)-1 {
					selected++
				}
			}
		} else if n == 3 && buf[0] == 27 && buf[1] == 91 {
			switch buf[2] {
			case 65: // Arrow up
				if selected > 0 {
					selected--
				}
			case 66: // Arrow down
				if selected < len(labels)-1 {
					selected++
				}
			}
		}

		render()
	}

	return selected
}
