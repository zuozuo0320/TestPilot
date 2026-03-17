package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config stores runtime settings for the demo service.
type Config struct {
	AppPort            string
	LogLevel           string
	DBHost             string
	DBPort             string
	DBUser             string
	DBPassword         string
	DBName             string
	DBMaxOpenConns     int
	DBMaxIdleConns     int
	DBConnMaxLifetime  int
	DBConnectRetries   int
	DBRetryDelaySecond int
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	AutoSeed           bool
	RunFailRate        float64
	CORSAllowOrigins   string
	JWTSecret          string
}

func Load() Config {
	return Config{
		AppPort:            getEnv("APP_PORT", "8080"),
		LogLevel:           strings.ToLower(getEnv("LOG_LEVEL", "info")),
		DBHost:             getEnv("DB_HOST", "127.0.0.1"),
		DBPort:             getEnv("DB_PORT", "3306"),
		DBUser:             getEnv("DB_USER", "testpilot"),
		DBPassword:         getEnv("DB_PASSWORD", "testpilot"),
		DBName:             getEnv("DB_NAME", "testpilot"),
		DBMaxOpenConns:     getEnvInt("DB_MAX_OPEN_CONNS", 20),
		DBMaxIdleConns:     getEnvInt("DB_MAX_IDLE_CONNS", 10),
		DBConnMaxLifetime:  getEnvInt("DB_CONN_MAX_LIFETIME_MIN", 60),
		DBConnectRetries:   getEnvInt("DB_CONNECT_RETRIES", 20),
		DBRetryDelaySecond: getEnvInt("DB_RETRY_DELAY_SEC", 2),
		RedisAddr:          getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:      getEnv("REDIS_PASSWORD", ""),
		RedisDB:            getEnvInt("REDIS_DB", 0),
		AutoSeed:           getEnvBool("AUTO_SEED", false),
		RunFailRate:        getEnvFloat("RUN_FAIL_RATE", 0.25),
		CORSAllowOrigins:   getEnv("CORS_ALLOW_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173,http://localhost:3000,http://127.0.0.1:3000"),
		JWTSecret:          getEnv("JWT_SECRET", "testpilot-dev-secret-change-in-production"),
	}
}

func (c Config) HTTPAddr() string {
	if strings.HasPrefix(c.AppPort, ":") {
		return c.AppPort
	}
	return ":" + c.AppPort
}

// Validate 校验必填配置，缺失时返回错误
func (c Config) Validate() error {
	if c.DBHost == "" {
		return fmt.Errorf("DB_HOST is required")
	}
	if c.DBUser == "" {
		return fmt.Errorf("DB_USER is required")
	}
	if c.DBName == "" {
		return fmt.Errorf("DB_NAME is required")
	}
	if c.AppPort == "" {
		return fmt.Errorf("APP_PORT is required")
	}
	return nil
}

func (c Config) MySQLDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.DBUser,
		c.DBPassword,
		c.DBHost,
		c.DBPort,
		c.DBName,
	)
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := getEnv(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.ToLower(getEnv(key, ""))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvFloat(key string, fallback float64) float64 {
	value := getEnv(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
