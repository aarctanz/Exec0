package config

import (
	"log"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Primary  PrimaryConfig  `env-prefix:"PRIMARY_"`
	Server   ServerConfig   `env-prefix:"SERVER_"`
	Database DatabaseConfig `env-prefix:"DATABASE_"`
}

type PrimaryConfig struct {
	Env string `env:"ENV" env-required:"true"`
}

type ServerConfig struct {
	Port               string   `env:"PORT" env-required:"true"`
	ReadTimeout        int      `env:"READ_TIMEOUT" env-required:"true"`
	WriteTimeout       int      `env:"WRITE_TIMEOUT" env-required:"true"`
	IdleTimeout        int      `env:"IDLE_TIMEOUT" env-required:"true"`
	CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" env-required:"true"`
}

type DatabaseConfig struct {
	Host            string `env:"HOST" env-required:"true"`
	Port            int    `env:"PORT" env-required:"true"`
	User            string `env:"USER" env-required:"true"`
	Password        string `env:"PASSWORD"`
	Name            string `env:"NAME" env-required:"true"`
	SSLMode         string `env:"SSL_MODE" env-required:"true"`
	MaxOpenConns    int    `env:"MAX_OPEN_CONNS" env-required:"true"`
	MaxIdleConns    int    `env:"MAX_IDLE_CONNS" env-required:"true"`
	ConnMaxLifetime int    `env:"CONN_MAX_LIFETIME" env-required:"true"`
	ConnMaxIdleTime int    `env:"CONN_MAX_IDLE_TIME" env-required:"true"`
}

func LoadConfig() (*Config, error) {
	var cfg Config

	err := cleanenv.ReadConfig(".env", &cfg)
	if err != nil {
		log.Fatal("could not load initial env variables")
	}

	return &cfg, nil
}
