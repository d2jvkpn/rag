package app

import (
	"log"
	"path/filepath"

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
	return v
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("http.addr", ":8080")
	v.SetDefault("http.jwt_secret", "change-me-in-production")
	v.SetDefault("http.session_cookie", "rag_session")
	v.SetDefault("app.data_dir", "data")
	v.SetDefault("admin.username", "admin")
	v.SetDefault("admin.password", "admin123")
	v.SetDefault("embedder.model", "text-embedding-3-small")
	v.SetDefault("milvus.collection", "rag_chunks")
	v.SetDefault("milvus.dim", 1536)
	v.SetDefault("llm.model", "gpt-4o-mini")
}
