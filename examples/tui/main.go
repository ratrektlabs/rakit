package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "agent server URL")
	flag.Parse()

	client := NewClient(*url)

	// Quick health check
	_, err := client.GetProvider(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot connect to %s: %v\n", *url, err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(client))

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
