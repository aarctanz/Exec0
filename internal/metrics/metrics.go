package metrics

import "github.com/prometheus/client_golang/prometheus"

// ---------- HTTP (API server) ----------

var HTTPRequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "exec0_http_requests_total",
		Help: "Total number of HTTP requests.",
	},
	[]string{"route", "method", "status_class"},
)

var HTTPRequestDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "exec0_http_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	},
	[]string{"route", "method"},
)

// ---------- Submissions (API server) ----------

var SubmissionsCreatedTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "exec0_submissions_created_total",
		Help: "Total submissions created.",
	},
	[]string{"language"},
)

var EnqueueFailuresTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "exec0_enqueue_failures_total",
		Help: "Total failures when enqueuing submissions to the task queue.",
	},
)

// ---------- Jobs (Worker) ----------

var JobsProcessedTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "exec0_jobs_processed_total",
		Help: "Total jobs processed by the worker.",
	},
	[]string{"status", "language"},
)

var JobDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "exec0_job_duration_seconds",
		Help:    "Job execution duration in seconds.",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60},
	},
	[]string{"phase", "language"},
)

var JobQueueWait = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "exec0_job_queue_wait_seconds",
		Help:    "Time a job spent waiting in the queue before execution.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	},
	[]string{"queue"},
)

var JobRetriesTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "exec0_job_retries_total",
		Help: "Total job retries.",
	},
	[]string{"reason"},
)

// ---------- Worker state ----------

var WorkerActiveJobs = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "exec0_worker_active_jobs",
		Help: "Number of jobs currently being processed.",
	},
)

var WorkerConcurrency = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "exec0_worker_concurrency",
		Help: "Configured worker concurrency.",
	},
)

// ---------- Sandbox ----------

var SandboxFailuresTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "exec0_sandbox_failures_total",
		Help: "Total sandbox operation failures.",
	},
	[]string{"stage"},
)

// ---------- Database ----------

var DBFailuresTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "exec0_db_failures_total",
		Help: "Total database operation failures.",
	},
	[]string{"operation"},
)

var DBOperationDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "exec0_db_operation_duration_seconds",
		Help:    "Database operation duration in seconds.",
		Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	},
	[]string{"operation"},
)

// RegisterAPI registers metrics relevant to the API server.
func RegisterAPI(reg prometheus.Registerer) {
	reg.MustRegister(
		HTTPRequestsTotal,
		HTTPRequestDuration,
		SubmissionsCreatedTotal,
		EnqueueFailuresTotal,
		DBFailuresTotal,
		DBOperationDuration,
	)
}

// RegisterWorker registers metrics relevant to the worker.
func RegisterWorker(reg prometheus.Registerer) {
	reg.MustRegister(
		JobsProcessedTotal,
		JobDuration,
		JobQueueWait,
		JobRetriesTotal,
		WorkerActiveJobs,
		WorkerConcurrency,
		SandboxFailuresTotal,
		DBFailuresTotal,
		DBOperationDuration,
	)
}
