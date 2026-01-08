package agent

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	cpuUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "namespace_quota_cpu_usage_usec",
			Help: "Current CPU usage in microseconds for the namespace",
		},
		[]string{"namespace"},
	)

	cpuLimit = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "namespace_quota_cpu_limit_usec",
			Help: "CPU limit in microseconds for the namespace",
		},
		[]string{"namespace"},
	)

	cpuThrottledPeriods = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "namespace_quota_cpu_throttled_periods",
			Help: "Number of CPU throttled periods for the namespace",
		},
		[]string{"namespace"},
	)

	memoryUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "namespace_quota_memory_usage_bytes",
			Help: "Current memory usage in bytes for the namespace",
		},
		[]string{"namespace"},
	)

	memoryLimit = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "namespace_quota_memory_limit_bytes",
			Help: "Memory limit in bytes for the namespace",
		},
		[]string{"namespace"},
	)

	oomKills = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "namespace_quota_oom_kills_total",
			Help: "Total number of OOM kills for the namespace",
		},
		[]string{"namespace"},
	)
)

func init() {
	prometheus.MustRegister(cpuUsage)
	prometheus.MustRegister(cpuLimit)
	prometheus.MustRegister(cpuThrottledPeriods)
	prometheus.MustRegister(memoryUsage)
	prometheus.MustRegister(memoryLimit)
	prometheus.MustRegister(oomKills)
}

type MetricsServer struct {
	cgroupManager *CgroupManager
	log           *logrus.Logger
	port          string
}

func NewMetricsServer(cgroupManager *CgroupManager, port string, log *logrus.Logger) *MetricsServer {
	return &MetricsServer{
		cgroupManager: cgroupManager,
		log:           log,
		port:          port,
	}
}

func (m *MetricsServer) Start() error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	m.log.WithField("port", m.port).Info("Starting metrics server")

	go func() {
		if err := http.ListenAndServe(":"+m.port, mux); err != nil {
			m.log.WithError(err).Error("Metrics server error")
		}
	}()

	return nil
}

func (m *MetricsServer) UpdateMetrics(namespace string, stats *CgroupStats, cpuLimitUsec, memoryLimitBytes int64) {
	cpuUsage.WithLabelValues(namespace).Set(float64(stats.CPUUsageUsec))
	cpuLimit.WithLabelValues(namespace).Set(float64(cpuLimitUsec))
	cpuThrottledPeriods.WithLabelValues(namespace).Set(float64(stats.CPUThrottled))
	memoryUsage.WithLabelValues(namespace).Set(float64(stats.MemoryUsageBytes))
	memoryLimit.WithLabelValues(namespace).Set(float64(memoryLimitBytes))
	oomKills.WithLabelValues(namespace).Set(float64(stats.OOMKills))
}

func (m *MetricsServer) ReadCgroupStats(namespace string) (*CgroupStats, error) {
	slicePath := m.cgroupManager.GetSlicePath(namespace)
	stats := &CgroupStats{}

	cpuStatPath := filepath.Join(slicePath, "cpu.stat")
	if content, err := os.ReadFile(cpuStatPath); err == nil {
		for _, line := range strings.Split(string(content), "\n") {
			parts := strings.Fields(line)
			if len(parts) != 2 {
				continue
			}
			value, _ := strconv.ParseInt(parts[1], 10, 64)
			switch parts[0] {
			case "usage_usec":
				stats.CPUUsageUsec = value
			case "nr_throttled":
				stats.CPUThrottled = value
			}
		}
	}

	memoryCurrentPath := filepath.Join(slicePath, "memory.current")
	if content, err := os.ReadFile(memoryCurrentPath); err == nil {
		stats.MemoryUsageBytes, _ = strconv.ParseInt(strings.TrimSpace(string(content)), 10, 64)
	}

	memoryEventsPath := filepath.Join(slicePath, "memory.events")
	if content, err := os.ReadFile(memoryEventsPath); err == nil {
		for _, line := range strings.Split(string(content), "\n") {
			parts := strings.Fields(line)
			if len(parts) == 2 && parts[0] == "oom_kill" {
				stats.OOMKills, _ = strconv.ParseInt(parts[1], 10, 64)
				break
			}
		}
	}

	return stats, nil
}
