package config

import (
	"log"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Release       bool
	ConfigPath    string
	HTTPAddr      string
	DataDir       string
	StatePath     string
	DatabaseDSN   string
	JWTSecret     string
	SessionCookie string
	AdminUsername string
	AdminPassword string
}

type LoadOptions struct {
	Release    bool
	ConfigPath string
	HTTPAddr   string
}

func Load(opts LoadOptions) Config {
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = filepath.Join("configs", "local.yaml")
	}

	v := viper.New()
	v.SetConfigFile(configPath)

	if err := v.ReadInConfig(); err != nil {
		log.Fatalf("read config %s: %v", configPath, err)
	}

	dataDir := v.GetString("data_dir")
	if dataDir == "" {
		dataDir = "data"
	}

	statePath := v.GetString("state_path")
	if statePath == "" {
		statePath = filepath.Join(dataDir, "app-state.json")
	}

	httpAddr := v.GetString("http_addr")
	if httpAddr == "" {
		httpAddr = ":8080"
	}
	if opts.HTTPAddr != "" {
		httpAddr = opts.HTTPAddr
	}

	databaseDSN := v.GetString("database.dsn")

	jwtSecret := v.GetString("jwt.secret")
	if jwtSecret == "" {
		jwtSecret = "change-me-in-production"
	}

	sessionCookie := v.GetString("session_cookie")
	if sessionCookie == "" {
		sessionCookie = "rag_session"
	}

	adminUsername := v.GetString("admin.username")
	if adminUsername == "" {
		adminUsername = "admin"
	}

	adminPassword := v.GetString("admin.password")
	if adminPassword == "" {
		adminPassword = "admin123"
	}

	return Config{
		Release:       opts.Release,
		ConfigPath:    configPath,
		HTTPAddr:      httpAddr,
		DataDir:       dataDir,
		StatePath:     statePath,
		DatabaseDSN:   databaseDSN,
		JWTSecret:     jwtSecret,
		SessionCookie: sessionCookie,
		AdminUsername: adminUsername,
		AdminPassword: adminPassword,
	}
}
