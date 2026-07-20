package infra

import (
	"net/url"

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

// RedactDSN masks the password component of a connection-string DSN (e.g. redis://) so it is
// safe to write to logs. DSNs that fail to parse as a URL are returned unchanged.
func RedactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	return u.Redacted()
}
