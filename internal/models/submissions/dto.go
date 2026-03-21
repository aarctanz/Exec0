package submissions

type CreateSubmissionDTO struct {
	LanguageID                           int64    `json:"language_id"`
	SourceCode                           string   `json:"source_code"`
	Stdin                                *string  `json:"stdin"`
	ExpectedOutput                       *string  `json:"expected_output"`
	CpuTimeLimit                         *float64 `json:"cpu_time_limit"`
	WallTimeLimit                        *float64 `json:"wall_time_limit"`
	MemoryLimit                          *int32   `json:"memory_limit"`
	StackLimit                           *int32   `json:"stack_limit"`
	MaxProcessesAndOrThreads             *int32   `json:"max_processes_and_or_threads"`
	EnablePerProcessAndThreadTimeLimit   *bool    `json:"enable_per_process_and_thread_time_limit"`
	EnablePerProcessAndThreadMemoryLimit *bool    `json:"enable_per_process_and_thread_memory_limit"`
	MaxFileSize                          *int32   `json:"max_file_size"`
	NumberOfRuns                         *int32   `json:"number_of_runs"`
	RedirectStderrToStdout               *bool    `json:"redirect_stderr_to_stdout"`
	EnableNetwork                        *bool    `json:"enable_network"`
}

func NewCreateSubmissionDTO() CreateSubmissionDTO {
	return CreateSubmissionDTO{}
}

type TestCaseInput struct {
	Stdin          string  `json:"stdin"`
	ExpectedOutput *string `json:"expected_output"`
}

type CreateBatchSubmissionDTO struct {
	LanguageID                           int64            `json:"language_id"`
	SourceCode                           string           `json:"source_code"`
	TestCases                            []TestCaseInput  `json:"test_cases"`
	CpuTimeLimit                         *float64         `json:"cpu_time_limit"`
	WallTimeLimit                        *float64         `json:"wall_time_limit"`
	MemoryLimit                          *int32           `json:"memory_limit"`
	StackLimit                           *int32           `json:"stack_limit"`
	MaxProcessesAndOrThreads             *int32           `json:"max_processes_and_or_threads"`
	EnablePerProcessAndThreadTimeLimit   *bool            `json:"enable_per_process_and_thread_time_limit"`
	EnablePerProcessAndThreadMemoryLimit *bool            `json:"enable_per_process_and_thread_memory_limit"`
	MaxFileSize                          *int32           `json:"max_file_size"`
	RedirectStderrToStdout               *bool            `json:"redirect_stderr_to_stdout"`
	EnableNetwork                        *bool            `json:"enable_network"`
}
