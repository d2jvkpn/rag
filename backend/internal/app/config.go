package app

import (
	"log"
	"strings"

	"github.com/spf13/viper"
)

func LoadConfig(path string) (config *viper.Viper) {
	var err error

	config = viper.New()
	config.SetConfigFile(path)

	if err = config.ReadInConfig(); err != nil {
		log.Fatalf("read config %s: %v", path, err)
	}

	config.SetDefault("app.data_dir", "data")

	basePath := strings.TrimRight(config.GetString("http.base_path"), "/")
	// basePath := filepath.Join(config.GetString("http.base_path"), "/")
	config.Set("http.base_path", basePath)

	config.SetDefault("http.jwt_secret", "change-me-in-production")
	config.SetDefault("http.session_cookie", "rag")
	config.SetDefault("http.allow_origins", []string{"*"})

	config.SetDefault("admin.username", "admin")
	config.SetDefault("admin.password", "admin123")
	config.SetDefault("embedder.model", "text-embedding-v3")
	config.SetDefault("embedder.batch_size", 10)
	config.SetDefault("milvus.api_key", "")

	if config.GetInt("embedder.dim") <= 0 {
		log.Fatal("embedder.dim is required and must be positive")
	}

	v := config.GetStringSlice("http.allow_origins")
	if len(v) == 0 {
		log.Fatal("http.allow_origins must not be empty")
	}

	return config
}
