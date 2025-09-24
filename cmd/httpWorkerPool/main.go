package main

import (
	"httpWorkerPool/internal/app/httpapp"
	"httpWorkerPool/internal/config"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// Считываем конфигурацию из переменных окружения, конфигурируем логгер и приложение
// Запускаем приложение и ждем сигнала о завершении от ОС
// Когда пришел сигнал о завершении запускаем плавную остановку приложения
func main() {
	cfg := config.MustLoad()

	log := setupLogger(cfg.Env)

	application := httpapp.New(cfg, log)

	go application.MustRun()

	stop := make(chan os.Signal)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	sign := <-stop

	log.Info("stopping application", slog.String("signal", sign.String()))

	application.Stop()

	log.Info("application stopped")

}

func setupLogger(env string) *slog.Logger {
	var log *slog.Logger

	switch env {
	case config.EnvLocal:
		log = slog.New(
			slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	case config.EnvDev:
		log = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	case config.EnvProd:
		log = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
		)
	}

	return log
}
