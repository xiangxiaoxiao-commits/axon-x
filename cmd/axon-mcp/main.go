// Command axon-mcp is a stdio Model Context Protocol (MCP) server that exposes
// axon's per-project knowledge graphs to MCP-capable CLI agents (Claude Code and
// friends). It is a standalone binary with no Wails/App dependency: it reads the
// same on-disk graphs/ and graphcache/ under the app data dir that the GUI
// builds, and reuses the shared recall core in internal/retrieve.
//
// Register with Claude Code:
//
//	claude mcp add axon-knowledge /path/to/axon-mcp
//
// Then, in a session, the agent can call the search_knowledge / list_projects /
// get_entity tools to query business knowledge distilled from past work.
package main

import (
	"bufio"
	"context"
	"log"
	"os"

	"axon/internal/config"
	"axon/internal/db"
	"axon/internal/secret"
)

func main() {
	// Logs go to stderr so they never corrupt the JSON-RPC stream on stdout.
	log.SetOutput(os.Stderr)
	log.SetPrefix("axon-mcp: ")
	log.SetFlags(0)

	dataDir, err := db.AppDataDir()
	if err != nil {
		log.Fatalf("resolve app data dir: %v", err)
	}
	cfg, err := config.Load(dataDir)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	h := &toolHandler{
		ctx:     context.Background(),
		dataDir: dataDir,
		cfg:     cfg,
		secrets: secret.NewKeychainStore(),
	}
	s := &server{
		w:    bufio.NewWriter(os.Stdout),
		tool: h,
	}
	if err := s.serve(os.Stdin); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
