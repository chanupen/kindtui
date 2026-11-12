package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/chanupen/kindtui/internal/ui"
)

var version = "0.3.0"

const usage = `kindtui - terminal UI for kind (Kubernetes in Docker) clusters

Usage:
  kindtui              start the UI (needs a terminal)
  kindtui --version    print the version
  kindtui --help       print this help
  kindtui --cheatsheet print every action, key, : command and shell equivalent (Markdown)

In the UI:
  tab        switch between the clusters and nodes panes
  enter      clusters: open nodes   nodes: log in to the node
  n s x r    create, start, stop, restart
  l b d p    node log, kubelet journal, describe, pods on node
  :          command line (:shell NODE, :logs NODE, :help ...)
  ctrl-p     command palette: search and run any action
  K          command reference with the equivalent shell commands
  ?          all keys
  q          quit

Environment:
  KIND_CONFIG   kind config used for the "repo config" create shape

Docs: https://github.com/chanupen/kindtui
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Println("kindtui", version)
			return
		case "--cheatsheet", "cheatsheet":
			fmt.Print(ui.Cheatsheet())
			return
		case "--help", "-h", "help":
			fmt.Print(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown argument %q\n", os.Args[1])
			os.Exit(2)
		}
	}
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "kindtui needs a terminal. Run it in a TTY, then press ? for keys.")
		os.Exit(1)
	}
	p := tea.NewProgram(ui.New(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "kindtui: %v\n", err)
		os.Exit(1)
	}
}
