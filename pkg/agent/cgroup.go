package agent

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	DefaultCPUPeriod    = 100000
	RequiredControllers = "+cpu +memory +pids"
)

type CgroupStats struct {
	CPUUsageUsec     int64
	CPUThrottled     int64
	MemoryUsageBytes int64
	OOMKills         int64
}

type CgroupManager struct {
	cgroupRoot  string
	slicePrefix string
	log         *logrus.Logger
}

func NewCgroupManager(cgroupRoot, slicePrefix string, log *logrus.Logger) *CgroupManager {
	return &CgroupManager{
		cgroupRoot:  cgroupRoot,
		slicePrefix: slicePrefix,
		log:         log,
	}
}

// GetSlicePath returns the cgroup path using systemd nested slice format: parent-child.slice
func (m *CgroupManager) GetSlicePath(namespace string) string {
	prefix := strings.TrimSuffix(m.slicePrefix, ".slice")
	sliceName := fmt.Sprintf("%s-%s.slice", prefix, namespace)
	return filepath.Join(m.cgroupRoot, m.slicePrefix, sliceName)
}

func (m *CgroupManager) GetParentSlicePath() string {
	return filepath.Join(m.cgroupRoot, m.slicePrefix)
}

func (m *CgroupManager) EnsureSlice(namespace string, cpuLimit string, memoryLimit string) error {
	slicePath := m.GetSlicePath(namespace)
	parentPath := m.GetParentSlicePath()

	m.log.WithFields(logrus.Fields{
		"namespace":    namespace,
		"slice_path":   slicePath,
		"cpu_limit":    cpuLimit,
		"memory_limit": memoryLimit,
	}).Debug("Ensuring cgroup slice")

	if err := m.ensureParentSlice(parentPath); err != nil {
		return fmt.Errorf("failed to ensure parent slice for %s: %w", namespace, err)
	}

	if err := os.MkdirAll(slicePath, 0755); err != nil {
		return fmt.Errorf("failed to create slice directory for %s: %w", namespace, err)
	}

	if err := m.enableControllers(slicePath); err != nil {
		m.log.WithError(err).Warn("Failed to enable controllers in namespace slice (may not have children)")
	}

	if cpuLimit != "" {
		cpuQuota, err := ParseCPU(cpuLimit)
		if err != nil {
			return fmt.Errorf("failed to parse CPU limit for %s: %w", namespace, err)
		}
		if err := m.setCPULimitViaSystemd(namespace, cpuQuota); err != nil {
			return fmt.Errorf("failed to set CPU limit for %s: %w", namespace, err)
		}
	}

	if memoryLimit != "" {
		memoryBytes, err := ParseMemory(memoryLimit)
		if err != nil {
			return fmt.Errorf("failed to parse memory limit for %s: %w", namespace, err)
		}
		if err := m.setMemoryLimitViaSystemd(namespace, memoryBytes); err != nil {
			return fmt.Errorf("failed to set memory limit for %s: %w", namespace, err)
		}
	}

	m.log.WithFields(logrus.Fields{
		"namespace":  namespace,
		"slice_path": slicePath,
	}).Info("Cgroup slice configured successfully")

	return nil
}

func (m *CgroupManager) RemoveSlice(namespace string) error {
	slicePath := m.GetSlicePath(namespace)

	m.log.WithFields(logrus.Fields{
		"namespace":  namespace,
		"slice_path": slicePath,
	}).Debug("Removing cgroup slice")

	if _, err := os.Stat(slicePath); os.IsNotExist(err) {
		m.log.WithField("namespace", namespace).Debug("Slice does not exist, nothing to remove")
		return nil
	}

	if err := os.Remove(slicePath); err != nil {
		return fmt.Errorf("failed to remove slice for %s: %w", namespace, err)
	}

	m.log.WithField("namespace", namespace).Info("Cgroup slice removed")
	return nil
}

func (m *CgroupManager) GetCgroupStats(namespace string) (*CgroupStats, error) {
	slicePath := m.GetSlicePath(namespace)

	if _, err := os.Stat(slicePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("slice does not exist for %s: %w", namespace, err)
	}

	stats := &CgroupStats{}

	cpuUsage, cpuThrottled, err := m.readCPUStat(slicePath)
	if err != nil {
		m.log.WithError(err).WithField("namespace", namespace).Debug("Failed to read cpu.stat")
	} else {
		stats.CPUUsageUsec = cpuUsage
		stats.CPUThrottled = cpuThrottled
	}

	memUsage, err := m.readMemoryCurrent(slicePath)
	if err != nil {
		m.log.WithError(err).WithField("namespace", namespace).Debug("Failed to read memory.current")
	} else {
		stats.MemoryUsageBytes = memUsage
	}

	oomKills, err := m.readMemoryEvents(slicePath)
	if err != nil {
		m.log.WithError(err).WithField("namespace", namespace).Debug("Failed to read memory.events")
	} else {
		stats.OOMKills = oomKills
	}

	return stats, nil
}

func (m *CgroupManager) SliceExists(namespace string) bool {
	slicePath := m.GetSlicePath(namespace)
	_, err := os.Stat(slicePath)
	return err == nil
}

func (m *CgroupManager) GetCurrentLimits(namespace string) (cpuQuota int64, memoryBytes int64, err error) {
	slicePath := m.GetSlicePath(namespace)

	cpuMaxPath := filepath.Join(slicePath, "cpu.max")
	cpuContent, err := os.ReadFile(cpuMaxPath)
	if err == nil {
		parts := strings.Fields(string(cpuContent))
		if len(parts) >= 1 && parts[0] != "max" {
			cpuQuota, _ = strconv.ParseInt(parts[0], 10, 64)
		}
	}

	memoryMaxPath := filepath.Join(slicePath, "memory.max")
	memoryContent, err := os.ReadFile(memoryMaxPath)
	if err == nil {
		memStr := strings.TrimSpace(string(memoryContent))
		if memStr != "max" {
			memoryBytes, _ = strconv.ParseInt(memStr, 10, 64)
		}
	}

	return cpuQuota, memoryBytes, nil
}

// ParseCPU converts CPU cores to microseconds quota (e.g., "4" -> 400000)
func ParseCPU(cpu string) (int64, error) {
	cpu = strings.TrimSpace(cpu)
	if cpu == "" {
		return 0, fmt.Errorf("empty CPU value")
	}

	cores, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU value '%s': %w", cpu, err)
	}

	if cores <= 0 {
		return 0, fmt.Errorf("CPU value must be positive: %s", cpu)
	}

	quota := int64(cores * float64(DefaultCPUPeriod))
	return quota, nil
}

// ParseMemory converts memory string to bytes (supports Ki, Mi, Gi, Ti suffixes)
func ParseMemory(memory string) (int64, error) {
	memory = strings.TrimSpace(memory)
	if memory == "" {
		return 0, fmt.Errorf("empty memory value")
	}

	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(Ki|Mi|Gi|Ti|K|M|G|T|k|m|g|t)?$`)
	matches := re.FindStringSubmatch(memory)
	if matches == nil {
		return 0, fmt.Errorf("invalid memory format: %s", memory)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory value: %w", err)
	}

	if value < 0 {
		return 0, fmt.Errorf("memory value must be non-negative: %s", memory)
	}

	suffix := strings.ToUpper(matches[2])

	var multiplier float64 = 1
	switch suffix {
	case "":
		multiplier = 1
	case "K", "KI":
		multiplier = 1024
	case "M", "MI":
		multiplier = 1024 * 1024
	case "G", "GI":
		multiplier = 1024 * 1024 * 1024
	case "T", "TI":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown memory suffix: %s", suffix)
	}

	bytes := int64(value * multiplier)
	return bytes, nil
}

func (m *CgroupManager) ensureParentSlice(parentPath string) error {
	if err := os.MkdirAll(parentPath, 0755); err != nil {
		return fmt.Errorf("failed to create parent slice %s: %w", parentPath, err)
	}
	return m.enableControllers(parentPath)
}

func (m *CgroupManager) enableControllers(path string) error {
	subtreeControl := filepath.Join(path, "cgroup.subtree_control")
	if err := os.WriteFile(subtreeControl, []byte(RequiredControllers), 0644); err != nil {
		return fmt.Errorf("failed to enable controllers in %s: %w", path, err)
	}
	return nil
}

func (m *CgroupManager) getSliceName(namespace string) string {
	prefix := strings.TrimSuffix(m.slicePrefix, ".slice")
	return fmt.Sprintf("%s-%s.slice", prefix, namespace)
}

// setCPULimitViaSystemd and setMemoryLimitViaSystemd use nsenter to run systemctl
// in the host namespace. This is required because systemd manages the cgroup hierarchy
// and silently ignores direct writes to cpu.max/memory.max files.
func (m *CgroupManager) setCPULimitViaSystemd(namespace string, cpuQuota int64) error {
	sliceName := m.getSliceName(namespace)
	cpuPercent := (cpuQuota * 100) / DefaultCPUPeriod

	m.log.WithFields(logrus.Fields{
		"slice":      sliceName,
		"cpuPercent": cpuPercent,
	}).Debug("Setting CPU limit via systemd")

	cmd := exec.Command("nsenter", "-t", "1", "-m", "-u", "-n", "--",
		"systemctl", "set-property", sliceName,
		fmt.Sprintf("CPUQuota=%d%%", cpuPercent),
		"--runtime")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set CPU via systemd for %s: %w, output: %s", namespace, err, string(output))
	}

	m.log.WithFields(logrus.Fields{
		"slice":      sliceName,
		"cpuPercent": cpuPercent,
	}).Info("CPU limit set via systemd")

	return nil
}

func (m *CgroupManager) setMemoryLimitViaSystemd(namespace string, memoryBytes int64) error {
	sliceName := m.getSliceName(namespace)
	memoryStr := formatMemoryForSystemd(memoryBytes)

	m.log.WithFields(logrus.Fields{
		"slice":  sliceName,
		"memory": memoryStr,
	}).Debug("Setting memory limit via systemd")

	cmd := exec.Command("nsenter", "-t", "1", "-m", "-u", "-n", "--",
		"systemctl", "set-property", sliceName,
		fmt.Sprintf("MemoryMax=%s", memoryStr),
		"--runtime")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set memory via systemd for %s: %w, output: %s", namespace, err, string(output))
	}

	m.log.WithFields(logrus.Fields{
		"slice":  sliceName,
		"memory": memoryStr,
	}).Info("Memory limit set via systemd")

	return nil
}

func formatMemoryForSystemd(bytes int64) string {
	const (
		GB = 1024 * 1024 * 1024
		MB = 1024 * 1024
		KB = 1024
	)

	if bytes >= GB && bytes%GB == 0 {
		return fmt.Sprintf("%dG", bytes/GB)
	}
	if bytes >= MB && bytes%MB == 0 {
		return fmt.Sprintf("%dM", bytes/MB)
	}
	if bytes >= KB && bytes%KB == 0 {
		return fmt.Sprintf("%dK", bytes/KB)
	}
	return fmt.Sprintf("%d", bytes)
}

func (m *CgroupManager) readCPUStat(slicePath string) (usageUsec, throttled int64, err error) {
	cpuStatPath := filepath.Join(slicePath, "cpu.stat")
	file, err := os.Open(cpuStatPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open cpu.stat: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "usage_usec":
			usageUsec = val
		case "nr_throttled":
			throttled = val
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, 0, fmt.Errorf("failed to read cpu.stat: %w", err)
	}

	return usageUsec, throttled, nil
}

func (m *CgroupManager) readMemoryCurrent(slicePath string) (int64, error) {
	memoryCurrentPath := filepath.Join(slicePath, "memory.current")
	content, err := os.ReadFile(memoryCurrentPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read memory.current: %w", err)
	}

	memStr := strings.TrimSpace(string(content))
	memBytes, err := strconv.ParseInt(memStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory.current: %w", err)
	}

	return memBytes, nil
}

func (m *CgroupManager) readMemoryEvents(slicePath string) (int64, error) {
	memoryEventsPath := filepath.Join(slicePath, "memory.events")
	file, err := os.Open(memoryEventsPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open memory.events: %w", err)
	}
	defer file.Close()

	var oomKills int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "oom_kill" {
			oomKills, _ = strconv.ParseInt(fields[1], 10, 64)
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("failed to read memory.events: %w", err)
	}

	return oomKills, nil
}
