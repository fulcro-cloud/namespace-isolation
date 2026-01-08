package plugin

import (
	"context"
	"fmt"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/pkg/stub"
	"github.com/sirupsen/logrus"
)

const (
	DefaultPluginName = "namespace-isolator"
	DefaultPluginIdx  = "10"
)

type Plugin struct {
	stub  stub.Stub
	cache *QuotaCache
	log   *logrus.Entry
	name  string
	idx   string
}

type Config struct {
	Name       string
	Idx        string
	Kubeconfig string
}

func New(cfg Config, log *logrus.Logger) (*Plugin, error) {
	if cfg.Name == "" {
		cfg.Name = DefaultPluginName
	}
	if cfg.Idx == "" {
		cfg.Idx = DefaultPluginIdx
	}

	pluginLog := log.WithField("plugin", cfg.Name)

	cache, err := NewQuotaCache(cfg.Kubeconfig, pluginLog)
	if err != nil {
		return nil, fmt.Errorf("failed to create quota cache: %w", err)
	}

	p := &Plugin{
		cache: cache,
		log:   pluginLog,
		name:  cfg.Name,
		idx:   cfg.Idx,
	}

	opts := []stub.Option{
		stub.WithPluginName(cfg.Name),
		stub.WithPluginIdx(cfg.Idx),
	}

	s, err := stub.New(p, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create NRI stub: %w", err)
	}
	p.stub = s

	return p, nil
}

func (p *Plugin) Run(ctx context.Context) error {
	p.log.WithFields(logrus.Fields{
		"name": p.name,
		"idx":  p.idx,
	}).Info("Starting NRI plugin")

	if err := p.cache.Start(ctx); err != nil {
		return fmt.Errorf("failed to start quota cache: %w", err)
	}

	err := p.stub.Run(ctx)
	if err != nil {
		p.log.WithError(err).Error("NRI stub exited with error")
	}

	p.cache.Stop()
	return err
}

func (p *Plugin) Configure(_ context.Context, _, runtime, version string) (stub.EventMask, error) {
	p.log.WithFields(logrus.Fields{
		"runtime": runtime,
		"version": version,
	}).Info("Plugin configured")

	mask := api.EventMask(0)
	mask.Set(api.Event_RUN_POD_SANDBOX)
	mask.Set(api.Event_CREATE_CONTAINER)

	return stub.EventMask(mask), nil
}

func (p *Plugin) Synchronize(_ context.Context, pods []*api.PodSandbox, containers []*api.Container) ([]*api.ContainerUpdate, error) {
	p.log.WithFields(logrus.Fields{
		"pods":       len(pods),
		"containers": len(containers),
	}).Info("Synchronized with runtime")

	return nil, nil
}

func (p *Plugin) Shutdown(_ context.Context) {
	p.log.Info("Plugin shutdown requested")
}

func (p *Plugin) RunPodSandbox(_ context.Context, pod *api.PodSandbox) error {
	p.log.WithFields(logrus.Fields{
		"pod":       pod.GetName(),
		"namespace": pod.GetNamespace(),
	}).Debug("Pod sandbox created")

	return nil
}

// CreateContainer adjusts the container's cgroup path to route it to the namespace cgroup slice.
func (p *Plugin) CreateContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	ns := pod.GetNamespace()

	if !p.cache.HasQuota(ns) {
		return nil, nil, nil
	}

	// Systemd cgroup path format: "slice:prefix:name"
	sliceName := fmt.Sprintf("brasa-%s.slice", ns)
	cgroupPath := fmt.Sprintf("%s:cri-containerd:%s", sliceName, container.GetId())

	adjust := &api.ContainerAdjustment{}
	adjust.SetLinuxCgroupsPath(cgroupPath)

	p.log.WithFields(logrus.Fields{
		"pod":       pod.GetName(),
		"namespace": ns,
		"container": container.GetName(),
		"cgroup":    cgroupPath,
	}).Info("Routing container to namespace cgroup")

	return adjust, nil, nil
}
