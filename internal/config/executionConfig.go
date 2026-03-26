package config

type ExecutionConfig struct {
	DefaultCPUTime                float64
	MaxCPUTime                    float64
	DefaultCPUExtraTime           float64
	MaxCPUExtraTime               float64
	DefaultWallTime               float64
	MaxWallTime                   float64
	DefaultMemoryKB               int32
	MaxMemoryKB                   int32
	DefaultStackKB                int32
	MaxStackKB                    int32
	DefaultMaxProcessesAndThreads int32
	MaxMaxProcessesAndThreads     int32
	DefaultMaxFileSizeKB          int32
	MaxMaxFileSizeKB              int32
	DefaultNumberOfRuns           int32
	MaxNumberOfRuns               int32
	DefaultEnableNetwork          bool
	AllowEnableNetwork            bool
	MaxStdoutBytes                int
	MaxStderrBytes                int
}

func DefaultExecutionConfig() *ExecutionConfig {
	return &ExecutionConfig{
		DefaultCPUTime:                5,
		MaxCPUTime:                    15,
		DefaultCPUExtraTime:           1,
		MaxCPUExtraTime:               5,
		DefaultWallTime:               10,
		MaxWallTime:                   20,
		DefaultMemoryKB:               256000,
		MaxMemoryKB:                   512000,
		DefaultStackKB:                64000,
		MaxStackKB:                    128000,
		DefaultMaxProcessesAndThreads: 60,
		MaxMaxProcessesAndThreads:     120,
		DefaultMaxFileSizeKB:          4096,
		MaxMaxFileSizeKB:              8192,
		DefaultNumberOfRuns:           1,
		MaxNumberOfRuns:               20,
		DefaultEnableNetwork:          false,
		AllowEnableNetwork:            true,
		MaxStdoutBytes:                10485760,
		MaxStderrBytes:                10485760,
	}
}
