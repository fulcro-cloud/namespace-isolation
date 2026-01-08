package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/fulcro-cloud/namespace-isolation/pkg/plugin"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	var (
		pluginName string
		pluginIdx  string
		kubeconfig string
		logLevel   string
		logFormat  string
	)

	flag.StringVar(&pluginName, "name", plugin.DefaultPluginName, "NRI plugin name")
	flag.StringVar(&pluginIdx, "idx", plugin.DefaultPluginIdx, "NRI plugin index (determines priority)")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (uses in-cluster config if empty)")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&logFormat, "log-format", "json", "Log format (json, text)")
	flag.Parse()

	log := logrus.New()

	if logFormat == "json" {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	}

	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		log.WithError(err).Warn("Invalid log level, defaulting to info")
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	log.WithFields(logrus.Fields{
		"version":    version,
		"commit":     commit,
		"pluginName": pluginName,
		"pluginIdx":  pluginIdx,
	}).Info("Starting nri-namespace-isolator")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.WithField("signal", sig.String()).Info("Received shutdown signal")
		cancel()
	}()

	cfg := plugin.Config{
		Name:       pluginName,
		Idx:        pluginIdx,
		Kubeconfig: kubeconfig,
	}

	p, err := plugin.New(cfg, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to create plugin")
	}

	if err := p.Run(ctx); err != nil {
		log.WithError(err).Error("Plugin exited with error")
		os.Exit(1)
	}

	log.Info("Plugin shutdown complete")
}
