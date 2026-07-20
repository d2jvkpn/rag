package mcpserver

import (
	"log"

	"github.com/spf13/viper"
)

// Collection describes one Milvus collection this MCP server is allowed to search.
type Collection struct {
	Name        string `mapstructure:"name"`
	Description string `mapstructure:"description"`
}

// LoadConfig reads and validates the MCP server config at path.
//
// Unlike the main backend's config, there is no Noop fallback: a search
// server with no embedder or no collections to search has nothing useful to
// do, so missing required fields are fatal at startup rather than degrading
// silently.
func LoadConfig(path string) (config *viper.Viper) {
	var err error

	config = viper.New()
	config.SetConfigFile(path)

	if err = config.ReadInConfig(); err != nil {
		log.Fatalf("read config %s: %v", path, err)
	}

	config.SetDefault("embedder.batch_size", 10)

	if config.GetString("embedder.base_url") == "" {
		log.Fatal("embedder.base_url is required")
	}
	if config.GetString("embedder.api_key") == "" {
		log.Fatal("embedder.api_key is required")
	}
	if config.GetString("embedder.model") == "" {
		log.Fatal("embedder.model is required")
	}
	if config.GetInt("embedder.dim") <= 0 {
		log.Fatal("embedder.dim is required and must be positive")
	}

	if config.GetString("milvus.addr") == "" {
		log.Fatal("milvus.addr is required")
	}

	collections := Collections(config)
	if len(collections) == 0 {
		log.Fatal("milvus.collections must not be empty")
	}
	seen := make(map[string]bool, len(collections))
	for _, c := range collections {
		if c.Name == "" {
			log.Fatal("milvus.collections entries must have a non-empty name")
		}
		if seen[c.Name] {
			log.Fatalf("milvus.collections has a duplicate name %q", c.Name)
		}
		seen[c.Name] = true
	}

	return config
}

// Collections unmarshals the milvus.collections list from config.
func Collections(config *viper.Viper) []Collection {
	var collections []Collection
	_ = config.UnmarshalKey("milvus.collections", &collections)
	return collections
}
