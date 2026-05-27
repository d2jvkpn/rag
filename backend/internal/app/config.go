package app

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

func LoadConfig(path string) *viper.Viper {
	if path == "" {
		path = filepath.Join("configs", "local.yaml")
	}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		log.Fatalf("read config %s: %v", path, err)
	}
	setDefaults(v)
	validateConfig(v)
	return v
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("http.addr", ":3061")
	v.SetDefault("http.base_path", "")
	v.SetDefault("http.jwt_secret", "change-me-in-production")
	v.SetDefault("http.session_cookie", "rag_session")
	v.SetDefault("http.allow_origins", []string{"*"})
	v.SetDefault("app.data_dir", "data")
	v.SetDefault("admin.username", "admin")
	v.SetDefault("admin.password", "admin123")
	v.SetDefault("embedder.model", "text-embedding-3-small")
	v.SetDefault("embedder.batch_size", 10)
	v.SetDefault("milvus.collection", "rag_chunks")
	v.SetDefault("milvus.dim", 1536)
	v.SetDefault("llm.model", "gpt-4o-mini")
}

func validateConfig(v *viper.Viper) {
	for _, origin := range v.GetStringSlice("http.allow_origins") {
		if strings.TrimSpace(origin) != "" {
			return
		}
	}
	log.Fatal("http.allow_origins must not be empty")
}
