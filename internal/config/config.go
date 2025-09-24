package config

import (
	"os"
	"strconv"
)

const (
	EnvLocal = "local"
	EnvDev   = "dev"
	EnvProd  = "prod"
)

const (
	defaultWorkersNumber = 4
	defaultQueueSize     = 64
	defaultPort          = 8000
)

type Config struct {
	Env           string
	WorkersNumber int
	QueueSize     int
	Port          int
}

func MustLoad() *Config {
	workersNumber := defaultWorkersNumber
	queueSize := defaultQueueSize
	port := defaultPort
	env := EnvLocal

	if v := os.Getenv("WORKERS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			panic("invalid WORKERS environment variable")
		}
		workersNumber = n
	}

	if v := os.Getenv("QUEUE_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			panic("invalid QUEUE_SIZE environment variable")
		}
		queueSize = n
	}

	if v := os.Getenv("PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1024 || n > 65535 {
			panic("invalid PORT environment variable")
		}
		port = n
	}

	if v := os.Getenv("ENV"); v != "" {
		switch v {
		case EnvLocal, EnvDev, EnvProd:
			env = v
		default:
			panic("invalid ENV environment variable")
		}
	}

	return &Config{
		Env:           env,
		WorkersNumber: workersNumber,
		QueueSize:     queueSize,
		Port:          port,
	}
}
