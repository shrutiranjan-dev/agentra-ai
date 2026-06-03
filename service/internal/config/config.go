package config

import (
	"os"
)

type Config struct {
	Host            string
	Port            string
	AuthToken       string
	OllamaBaseURL   string
	MongoURI        string
	MongoDatabase   string
	Neo4jURI        string
	Neo4jUsername   string
	Neo4jPassword   string
	DefaultModel    string
	PermissionTTLMS int
}

func Load() Config {
	token := os.Getenv("APP_AUTH_TOKEN")
	if token == "" {
		token = "local-dev-token"
	}
	return Config{
		Host:            getenv("APP_HOST", "127.0.0.1"),
		Port:            getenv("APP_PORT", "8088"),
		AuthToken:       token,
		OllamaBaseURL:   getenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
		MongoURI:        getenv("MONGODB_URI", "mongodb://localhost:27017"),
		MongoDatabase:   getenv("MONGODB_DATABASE", "assistant"),
		Neo4jURI:        getenv("NEO4J_URI", "neo4j://localhost:7687"),
		Neo4jUsername:   getenv("NEO4J_USERNAME", "neo4j"),
		Neo4jPassword:   getenv("NEO4J_PASSWORD", "password12345"),
		DefaultModel:    getenv("OLLAMA_DEFAULT_MODEL", "llama3.1"),
		PermissionTTLMS: 60000,
	}
}

func (c Config) Address() string {
	return c.Host + ":" + c.Port
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
