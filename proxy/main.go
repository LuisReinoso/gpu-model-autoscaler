package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gpu_autoscaler_requests_total",
		Help: "Total proxied requests",
	})
	requestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "gpu_autoscaler_request_duration_seconds",
		Help:    "Request latency",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
	queueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gpu_autoscaler_queue_depth",
		Help: "Current request queue depth",
	})
	activeWorkers = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gpu_autoscaler_active_workers",
		Help: "Number of active vLLM workers",
	})
	gpuUtilization = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gpu_autoscaler_gpu_utilization",
		Help: "Average GPU utilization across workers",
	})
	scaleEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gpu_autoscaler_scale_events_total",
		Help: "Total scale events by direction",
	}, []string{"direction"})
)

type Worker struct {
	URL        string
	Healthy    bool
	Requests   int64
	mu         sync.RWMutex
}

type Proxy struct {
	workers      []*Worker
	mu           sync.RWMutex
	nextWorker   uint64
	autoscaler   *Autoscaler
}

type Autoscaler struct {
	minWorkers     int
	maxWorkers     int
	scaleUpThreshold   float64
	scaleDownThreshold float64
	cooldownScaleUp    time.Duration
	cooldownScaleDown  time.Duration
	lastScaleUp        time.Time
	lastScaleDown      time.Time
	mu                 sync.Mutex
	cloudProvider      CloudProvider
}

type CloudProvider interface {
	ScaleUp() (string, error)
	ScaleDown(workerID string) error
}

func NewProxy(workerURLs []string, autoscaler *Autoscaler) *Proxy {
	workers := make([]*Worker, len(workerURLs))
	for i, u := range workerURLs {
		workers[i] = &Worker{URL: u, Healthy: true}
	}
	return &Proxy{
		workers:    workers,
		autoscaler: autoscaler,
	}
}

func (p *Proxy) getLeastLoadedWorker() *Worker {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var best *Worker
	minReqs := int64(1<<63 - 1)
	for _, w := range p.workers {
		w.mu.RLock()
		reqs := w.Requests
		healthy := w.Healthy
		w.mu.RUnlock()
		if healthy && reqs < minReqs {
			minReqs = reqs
			best = w
		}
	}
	return best
}

func (p *Proxy) addWorker(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.workers = append(p.workers, &Worker{URL: url, Healthy: true})
	activeWorkers.Set(float64(len(p.workers)))
	log.Printf("[proxy] added worker: %s (total: %d)", url, len(p.workers))
}

func (p *Proxy) removeWorker(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, w := range p.workers {
		if w.URL == url {
			p.workers = append(p.workers[:i], p.workers[i+1:]...)
			activeWorkers.Set(float64(len(p.workers)))
			log.Printf("[proxy] removed worker: %s (total: %d)", url, len(p.workers))
			return
		}
	}
}

func (p *Proxy) healthCheck() {
	p.mu.RLock()
	workers := make([]*Worker, len(p.workers))
	copy(workers, p.workers)
	p.mu.RUnlock()

	totalGPU := 0.0
	healthyCount := 0

	for _, w := range workers {
		resp, err := http.Get(w.URL + "/health")
		w.mu.Lock()
		if err != nil || resp.StatusCode != 200 {
			w.Healthy = false
		} else {
			w.Healthy = true
			healthyCount++
			var health map[string]interface{}
			if resp.Body != nil {
				json.NewDecoder(resp.Body).Decode(&health)
				resp.Body.Close()
				if util, ok := health["gpu_utilization"].(float64); ok {
					totalGPU += util
				}
			}
		}
		w.mu.Unlock()
	}

	if healthyCount > 0 {
		gpuUtilization.Set(totalGPU / float64(healthyCount))
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy"}`))
		return
	}
	if r.URL.Path == "/metrics" {
		promhttp.Handler().ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/admin/workers" {
		p.handleAdminWorkers(w)
		return
	}
	if r.URL.Path == "/admin/scale/up" {
		p.handleManualScaleUp(w)
		return
	}
	if r.URL.Path == "/admin/scale/down" {
		p.handleManualScaleDown(w)
		return
	}

	requestsTotal.Inc()
	queueDepth.Inc()
	defer queueDepth.Dec()

	start := time.Now()

	worker := p.getLeastLoadedWorker()
	if worker == nil {
		http.Error(w, `{"error":"no healthy workers available"}`, http.StatusServiceUnavailable)
		return
	}

	atomic.AddInt64(&worker.Requests, 1)
	defer atomic.AddInt64(&worker.Requests, -1)

	target, _ := url.Parse(worker.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)

	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	r.Header.Set("X-Forwarded-By", "gpu-autoscaler-proxy")
	proxy.ServeHTTP(w, r)

	requestDuration.Observe(time.Since(start).Seconds())

	p.autoscaler.evaluate()
}

func (p *Proxy) handleAdminWorkers(w http.ResponseWriter) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	type WorkerInfo struct {
		URL     string `json:"url"`
		Healthy bool   `json:"healthy"`
		Load    int64  `json:"active_requests"`
	}
	workers := make([]WorkerInfo, len(p.workers))
	for i, w := range p.workers {
		w.mu.RLock()
		workers[i] = WorkerInfo{URL: w.URL, Healthy: w.Healthy, Load: w.Requests}
		w.mu.RUnlock()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workers": workers,
		"total":   len(workers),
	})
}

func (p *Proxy) handleManualScaleUp(w http.ResponseWriter) {
	url, err := p.autoscaler.scaleUp()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	p.addWorker(url)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "worker": url})
}

func (p *Proxy) handleManualScaleDown(w http.ResponseWriter) {
	p.mu.RLock()
	if len(p.workers) <= 1 {
		p.mu.RUnlock()
		http.Error(w, `{"error":"cannot scale below 1 worker"}`, http.StatusBadRequest)
		return
	}
	url := p.workers[len(p.workers)-1].URL
	p.mu.RUnlock()

	p.autoscaler.scaleDown(url)
	p.removeWorker(url)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "worker": url})
}

func (a *Autoscaler) evaluate() {
	queueVal := queueDepth
	workers := activeWorkers

	currentQueue := 0.0
	currentWorkers := 0.0

	// Use gauge snapshot (simplified)
	if m, err := prometheus.DefaultGatherer.Gather(); err == nil {
		for _, mf := range m {
			if mf.GetName() == "gpu_autoscaler_queue_depth" && len(mf.Metric) > 0 {
				currentQueue = mf.Metric[0].GetGauge().GetValue()
			}
			if mf.GetName() == "gpu_autoscaler_active_workers" && len(mf.Metric) > 0 {
				currentWorkers = mf.Metric[0].GetGauge().GetValue()
			}
		}
	}
	_ = queueVal
	_ = workers

	if currentWorkers == 0 {
		currentWorkers = float64(a.minWorkers)
	}

	loadPerWorker := currentQueue / currentWorkers

	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()

	if loadPerWorker > a.scaleUpThreshold &&
		int(currentWorkers) < a.maxWorkers &&
		now.Sub(a.lastScaleUp) > a.cooldownScaleUp {
		log.Printf("[autoscaler] load %.2f > threshold %.2f, scaling up (workers: %.0f/%.0f max: %d)",
			loadPerWorker, a.scaleUpThreshold, currentWorkers, activeWorkers, a.maxWorkers)

		if url, err := a.scaleUp(); err == nil {
			a.lastScaleUp = now
			a.lastScaleDown = now
			log.Printf("[autoscaler] scaled up: %s", url)
		}
	}

	if loadPerWorker < a.scaleDownThreshold &&
		int(currentWorkers) > a.minWorkers &&
		now.Sub(a.lastScaleDown) > a.cooldownScaleDown {
		log.Printf("[autoscaler] load %.2f < threshold %.2f, scaling down (workers: %.0f min: %d)",
			loadPerWorker, a.scaleDownThreshold, currentWorkers, a.minWorkers)
		a.lastScaleDown = now
	}
}

func (a *Autoscaler) scaleUp() (string, error) {
	scaleEvents.WithLabelValues("up").Inc()
	return a.cloudProvider.ScaleUp()
}

func (a *Autoscaler) scaleDown(workerID string) {
	scaleEvents.WithLabelValues("down").Inc()
	a.cloudProvider.ScaleDown(workerID)
}

func main() {
	prometheus.MustRegister(
		requestsTotal,
		requestDuration,
		queueDepth,
		activeWorkers,
		gpuUtilization,
		scaleEvents,
	)

	promauto := false
	http.Handle("/metrics", promhttp.Handler())
	_ = promauto

	workerURLs := getWorkerURLs()
	provider := detectCloudProvider()
	autoscaler := &Autoscaler{
		minWorkers:         getEnvInt("MIN_WORKERS", 1),
		maxWorkers:         getEnvInt("MAX_WORKERS", 8),
		scaleUpThreshold:   getEnvFloat("SCALE_UP_THRESHOLD", 3.0),
		scaleDownThreshold: getEnvFloat("SCALE_DOWN_THRESHOLD", 0.3),
		cooldownScaleUp:    getEnvDuration("COOLDOWN_SCALE_UP", 60*time.Second),
		cooldownScaleDown:  getEnvDuration("COOLDOWN_SCALE_DOWN", 120*time.Second),
		cloudProvider:      provider,
	}

	proxy := NewProxy(workerURLs, autoscaler)
	activeWorkers.Set(float64(len(workerURLs)))

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for range ticker.C {
			proxy.healthCheck()
		}
	}()

	port := getEnv("PROXY_PORT", "8080")
	log.Printf("[proxy] listening on :%s with %d initial workers", port, len(workerURLs))
	if err := http.ListenAndServe(":"+port, proxy); err != nil {
		log.Fatal(err)
	}
}

func getWorkerURLs() []string {
	workers := os.Getenv("WORKER_URLS")
	if workers == "" {
		return []string{"http://vllm-worker:8000"}
	}
	return strings.Split(workers, ",")
}

func detectCloudProvider() CloudProvider {
	provider := os.Getenv("CLOUD_PROVIDER")
	switch provider {
	case "runpod":
		return NewRunPodProvider()
	case "lambda":
		return NewLambdaProvider()
	default:
		return NewStubProvider()
	}
}

type StubProvider struct {
	counter int
}

func NewStubProvider() *StubProvider {
	return &StubProvider{counter: 1}
}

func (s *StubProvider) ScaleUp() (string, error) {
	s.counter++
	port := 8000 + s.counter
	url := fmt.Sprintf("http://vllm-worker-%d:%d", s.counter, port)
	log.Printf("[stub] scaled up: %s", url)
	return url, nil
}

func (s *StubProvider) ScaleDown(workerID string) error {
	log.Printf("[stub] scaled down: %s", workerID)
	return nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		fmt.Sscanf(v, "%d", &i)
		return i
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if v := os.Getenv(key); v != "" {
		var f float64
		fmt.Sscanf(v, "%f", &f)
		return f
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return defaultVal
}

func init() {
	rand.Seed(time.Now().UnixNano())
}