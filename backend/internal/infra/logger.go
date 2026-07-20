package infra

import (
	"github.com/spf13/viper"

	pkginfra "github.com/d2jvkpn/rag/backend/pkg/infra"
)

// L is initialized to a no-op logger so tests that skip Init() don't panic.
var L = pkginfra.L

func Init(config *viper.Viper) {
	pkginfra.Init(config)
	L = pkginfra.L
}

// EnableFileLogging upgrades the underlying pkginfra logger to also write to
// the log file, then re-syncs this package's L var to match.
func EnableFileLogging() {
	pkginfra.EnableFileLogging()
	L = pkginfra.L
}

func Sync() {
	pkginfra.Sync()
}
