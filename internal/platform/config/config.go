package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
}

// DatabaseConfig supports sqlite | postgres | mysql.
// Changing driver does not migrate data; 2.0 is fresh-install only.
type DatabaseConfig struct {
	Driver          string `mapstructure:"driver"`
	Path            string `mapstructure:"path"` // sqlite
	Host            string `mapstructure:"host"`
	Port            int    `mapstructure:"port"`
	Name            string `mapstructure:"name"`
	User            string `mapstructure:"user"`
	Password        string `mapstructure:"password"`
	SSLMode         string `mapstructure:"ssl_mode"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime string `mapstructure:"conn_max_lifetime"`
}

type JWTConfig struct {
	Secret     string `mapstructure:"secret"`
	AccessTTL  string `mapstructure:"access_ttl"`
	RefreshTTL string `mapstructure:"refresh_ttl"`
}

type BuildConfig struct {
	MaxConcurrent int    `mapstructure:"max_concurrent"`
	WorkspaceDir  string `mapstructure:"workspace_dir"`
	ArtifactDir   string `mapstructure:"artifact_dir"`
	LogDir        string `mapstructure:"log_dir"`
	CacheDir      string `mapstructure:"cache_dir"`
}

// StorageConfig controls the content-addressed upload store. Limits are bytes.
type StorageConfig struct {
	Root               string `mapstructure:"root"`
	AttachmentMaxBytes int64  `mapstructure:"attachment_max_bytes"`
	DocImportMaxBytes  int64  `mapstructure:"doc_import_max_bytes"`
}

type EncryptionConfig struct {
	Key string `mapstructure:"key"`
}

type AdminConfig struct {
	Username    string `mapstructure:"username"`
	Password    string `mapstructure:"password"`
	DisplayName string `mapstructure:"display_name"`
}

// DshConfig is the DSH subprocess settings. Listen address is not configurable.
type DshConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	Bin            string `mapstructure:"bin"`
	Home           string `mapstructure:"home"`
	WorkspaceRoot  string `mapstructure:"workspace_root"`
	Port           int    `mapstructure:"port"`
	StartupTimeout string `mapstructure:"startup_timeout"`
	HealthInterval string `mapstructure:"health_interval"`
	AutoRestart    bool   `mapstructure:"auto_restart"`
	ApprovalMode   string `mapstructure:"approval_mode"`
	PendingTTL     string `mapstructure:"pending_ttl"`
	SessionIdleTTL string `mapstructure:"session_idle_ttl"`
	LogDir         string `mapstructure:"log_dir"`
	MaxSessions    int    `mapstructure:"max_sessions"`
}

type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Database   DatabaseConfig   `mapstructure:"database"`
	JWT        JWTConfig        `mapstructure:"jwt"`
	Build      BuildConfig      `mapstructure:"build"`
	Storage    StorageConfig    `mapstructure:"storage"`
	Encryption EncryptionConfig `mapstructure:"encryption"`
	Admin      AdminConfig      `mapstructure:"admin"`
	Dsh        DshConfig        `mapstructure:"dsh"`
}

// C is the process-wide config after Load (nil until Load succeeds).
var C *Config

func Load(configPath string) (*Config, error) {
	v := viper.New()

	v.SetConfigName("config")
	v.SetConfigType("yaml")

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.AddConfigPath(".")
		v.AddConfigPath(filepath.Join(".", "config"))
	}

	v.SetDefault("server.port", 8080)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.path", "./data/db.sqlite")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.ssl_mode", "disable")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "1h")
	v.SetDefault("jwt.access_ttl", "2h")
	v.SetDefault("jwt.refresh_ttl", "168h")
	v.SetDefault("build.max_concurrent", 3)
	v.SetDefault("storage.root", "./data/storage")
	v.SetDefault("storage.attachment_max_bytes", 20*1024*1024)
	v.SetDefault("storage.doc_import_max_bytes", 100*1024*1024)
	v.SetDefault("dsh.enabled", false)
	v.SetDefault("dsh.bin", "dsh")
	v.SetDefault("dsh.port", 17800)
	v.SetDefault("dsh.startup_timeout", "60s")
	v.SetDefault("dsh.health_interval", "10s")
	v.SetDefault("dsh.auto_restart", true)
	v.SetDefault("dsh.approval_mode", "manual")
	v.SetDefault("dsh.pending_ttl", "10m")
	v.SetDefault("dsh.session_idle_ttl", "72h")
	v.SetDefault("dsh.max_sessions", 64)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, fmt.Errorf("config file not found")
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	v.SetEnvPrefix("BEDROCK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	cfg.Database.Driver = normalizeDriver(cfg.Database.Driver)

	configFile := v.ConfigFileUsed()
	configDir := filepath.Dir(configFile)
	if !filepath.IsAbs(configDir) {
		if abs, err := filepath.Abs(configDir); err == nil {
			configDir = abs
		}
	}

	cfg.Database.Path = resolvePath(configDir, cfg.Database.Path)
	cfg.Build.WorkspaceDir = resolvePath(configDir, cfg.Build.WorkspaceDir)
	cfg.Build.ArtifactDir = resolvePath(configDir, cfg.Build.ArtifactDir)
	cfg.Build.LogDir = resolvePath(configDir, cfg.Build.LogDir)
	cfg.Build.CacheDir = resolvePath(configDir, cfg.Build.CacheDir)
	cfg.Storage.Root = resolvePath(configDir, cfg.Storage.Root)
	cfg.Dsh.Home = resolvePath(configDir, cfg.Dsh.Home)
	cfg.Dsh.WorkspaceRoot = resolvePath(configDir, cfg.Dsh.WorkspaceRoot)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	C = &cfg
	return &cfg, nil
}

func normalizeDriver(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "", "sqlite", "sqlite3":
		return "sqlite"
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return strings.ToLower(strings.TrimSpace(d))
	}
}

func (c *Config) Validate() error {
	if c.JWT.Secret == "" {
		return fmt.Errorf("jwt.secret is required")
	}
	if c.Encryption.Key == "" {
		return fmt.Errorf("encryption.key is required")
	}
	switch c.Database.Driver {
	case "sqlite":
		if c.Database.Path == "" {
			return fmt.Errorf("database.path is required for sqlite")
		}
	case "postgres", "mysql":
		if c.Database.Host == "" {
			return fmt.Errorf("database.host is required for %s", c.Database.Driver)
		}
		if c.Database.Name == "" {
			return fmt.Errorf("database.name is required for %s", c.Database.Driver)
		}
		if c.Database.User == "" {
			return fmt.Errorf("database.user is required for %s", c.Database.Driver)
		}
	default:
		return fmt.Errorf("unsupported database.driver %q (want sqlite|postgres|mysql)", c.Database.Driver)
	}
	if c.Database.ConnMaxLifetime != "" {
		if _, err := time.ParseDuration(c.Database.ConnMaxLifetime); err != nil {
			return fmt.Errorf("invalid database.conn_max_lifetime: %w", err)
		}
	}
	if c.Storage.Root == "" {
		return fmt.Errorf("storage.root is required")
	}
	if c.Storage.AttachmentMaxBytes <= 0 {
		return fmt.Errorf("storage.attachment_max_bytes must be greater than zero")
	}
	if c.Storage.DocImportMaxBytes <= 0 {
		return fmt.Errorf("storage.doc_import_max_bytes must be greater than zero")
	}
	for _, item := range []struct {
		name  string
		value string
	}{
		{"dsh.startup_timeout", c.Dsh.StartupTimeout},
		{"dsh.health_interval", c.Dsh.HealthInterval},
		{"dsh.pending_ttl", c.Dsh.PendingTTL},
		{"dsh.session_idle_ttl", c.Dsh.SessionIdleTTL},
	} {
		if _, err := time.ParseDuration(item.value); err != nil {
			return fmt.Errorf("invalid %s: %w", item.name, err)
		}
	}
	return nil
}

func (c *DatabaseConfig) ConnMaxLifetimeDuration() time.Duration {
	if c.ConnMaxLifetime == "" {
		return time.Hour
	}
	d, err := time.ParseDuration(c.ConnMaxLifetime)
	if err != nil {
		return time.Hour
	}
	return d
}

func resolvePath(baseDir, targetPath string) string {
	if targetPath == "" || filepath.IsAbs(targetPath) {
		return targetPath
	}
	return filepath.Join(baseDir, targetPath)
}
