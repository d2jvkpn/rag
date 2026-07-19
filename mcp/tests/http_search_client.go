// Command http_search_client is a manual smoke-test client for the MCP
// retrieval server: it connects over Streamable HTTP to an already-running
// instance (see mcp/main.go, default :3062) and calls the "search" tool once,
// printing the raw result. Not a go test — run directly:
//
//	go run ./tests/http_search_client.go -collection example-kb -query "hello"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	var (
		endpoint   string
		collection string
		query      string
		searchMode string
		topK       int
		documentID string
		timeout    time.Duration
	)

	flag.StringVar(&endpoint, "endpoint", "http://127.0.0.1:3062", "MCP server base URL (Streamable HTTP)")
	flag.StringVar(&collection, "collection", "", "collection name to search (required)")
	flag.StringVar(&query, "query", "", "search query text (required)")
	flag.StringVar(&searchMode, "search-mode", "dense", "dense, bm25, or hybrid")
	flag.IntVar(&topK, "top-k", 5, "number of results to return")
	flag.StringVar(&documentID, "document-id", "", "optional: restrict results to this document ID")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "request timeout")
	flag.Parse()

	if collection == "" || query == "" {
		fmt.Fprintln(os.Stderr, "usage: http_search_client -collection <name> -query <text> [flags]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-http-test-client", Version: "v0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		log.Fatalf("connect to %s: %v", endpoint, err)
	}
	defer session.Close()

	args := map[string]any{
		"collection":  collection,
		"query":       query,
		"top_k":       topK,
		"search_mode": searchMode,
	}
	if documentID != "" {
		args["document_ids"] = []string{documentID}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: args,
	})
	if err != nil {
		log.Fatalf("call search tool: %v", err)
	}

	if result.IsError {
		var sb strings.Builder
		for _, c := range result.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				sb.WriteString(tc.Text)
			}
		}
		log.Fatalf("search tool returned an error: %s", sb.String())
	}

	out, err := json.MarshalIndent(result.StructuredContent, "", "  ")
	if err != nil {
		log.Fatalf("marshal structured content: %v", err)
	}
	fmt.Println(string(out))
}
