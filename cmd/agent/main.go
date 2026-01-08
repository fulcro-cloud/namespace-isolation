package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulcro-cloud/namespace-isolator/pkg/agent"
	"github.com/sirupsen/logrus"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig file (uses in-cluster config if empty)")
	cgroupRoot := flag.String("cgroup-root", "/sys/fs/cgroup", "Root path for cgroup v2 filesystem")
	slicePrefix := flag.String("slice-prefix", "brasa.slice", "Prefix for cgroup slice names")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	metricsPort := flag.String("metrics-port", "9090", "Port for Prometheus metrics server")
	flag.Parse()

	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
	})

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		log.WithError(err).Warn("Invalid log level, defaulting to info")
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	log.WithFields(logrus.Fields{
		"cgroup_root":  *cgroupRoot,
		"slice_prefix": *slicePrefix,
		"metrics_port": *metricsPort,
	}).Info("Starting nri-namespace-isolator agent")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		log.WithField("signal", sig.String()).Info("Received shutdown signal")
		cancel()
	}()

	cgroupManager := agent.NewCgroupManager(*cgroupRoot, *slicePrefix, log)

	metricsServer := agent.NewMetricsServer(cgroupManager, *metricsPort, log)
	if err := metricsServer.Start(); err != nil {
		log.WithError(err).Fatal("Failed to start metrics server")
	}

	config := agent.ControllerConfig{
		Kubeconfig:    *kubeconfig,
		CgroupRoot:    *cgroupRoot,
		SlicePrefix:   *slicePrefix,
		Log:           log,
		MetricsServer: metricsServer,
	}

	controller, err := agent.NewController(config)
	if err != nil {
		log.WithError(err).Fatal("Failed to create controller")
	}

	if err := controller.Run(ctx); err != nil {
		log.WithError(err).Fatal("Controller error")
	}

	log.Info("Agent shutdown complete")
}
