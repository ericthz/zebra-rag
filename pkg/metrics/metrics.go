// Package metrics 提供轻量运行指标，以 Prometheus 文本格式暴露（/metrics）。
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"sync/atomic"
	"time"
)

var (
	startTime       = time.Now()
	searchTotal     int64
	searchErrors    int64
	chatTotal       int64
	chatErrors      int64
	lastSearchNanos int64
)

// searchLatencyBuckets 检索耗时直方图分桶（毫秒）。
var searchLatencyBuckets = []int64{50, 100, 200, 500, 1000, 5000}

// searchLatencyCounts 各桶计数（含上界），长度 = len(buckets)+1（最后为 +Inf）。
var searchLatencyCounts = make([]int64, len(searchLatencyBuckets)+1)

// IncSearch 记录一次混合搜索请求。
func IncSearch() { atomic.AddInt64(&searchTotal, 1) }

// IncSearchError 记录一次混合搜索失败。
func IncSearchError() { atomic.AddInt64(&searchErrors, 1) }

// IncChat 记录一次对话请求。
func IncChat() { atomic.AddInt64(&chatTotal, 1) }

// IncChatError 记录一次对话失败。
func IncChatError() { atomic.AddInt64(&chatErrors, 1) }

// RecordSearchLatency 记录最近一次检索耗时（毫秒），并累加进直方图。
func RecordSearchLatency(ms int64) {
	atomic.StoreInt64(&lastSearchNanos, ms)
	idx := sort.Search(len(searchLatencyBuckets), func(i int) bool { return ms < searchLatencyBuckets[i] })
	atomic.AddInt64(&searchLatencyCounts[idx], 1)
}

// Handler 暴露 Prometheus 文本格式的运行指标。
func Handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP zebrarag_search_total Total hybrid search requests\n")
	fmt.Fprintf(w, "# TYPE zebrarag_search_total counter\n")
	fmt.Fprintf(w, "zebrarag_search_total %d\n", atomic.LoadInt64(&searchTotal))
	fmt.Fprintf(w, "# HELP zebrarag_search_errors Search errors\n")
	fmt.Fprintf(w, "# TYPE zebrarag_search_errors counter\n")
	fmt.Fprintf(w, "zebrarag_search_errors %d\n", atomic.LoadInt64(&searchErrors))
	fmt.Fprintf(w, "# HELP zebrarag_chat_total Total chat requests\n")
	fmt.Fprintf(w, "# TYPE zebrarag_chat_total counter\n")
	fmt.Fprintf(w, "zebrarag_chat_total %d\n", atomic.LoadInt64(&chatTotal))
	fmt.Fprintf(w, "# HELP zebrarag_chat_errors Chat errors\n")
	fmt.Fprintf(w, "# TYPE zebrarag_chat_errors counter\n")
	fmt.Fprintf(w, "zebrarag_chat_errors %d\n", atomic.LoadInt64(&chatErrors))
	fmt.Fprintf(w, "# HELP zebrarag_last_search_latency_ms Last search latency\n")
	fmt.Fprintf(w, "# TYPE zebrarag_last_search_latency_ms gauge\n")
	fmt.Fprintf(w, "zebrarag_last_search_latency_ms %d\n", atomic.LoadInt64(&lastSearchNanos))
	fmt.Fprintf(w, "# HELP zebrarag_search_latency_ms Search latency histogram\n")
	fmt.Fprintf(w, "# TYPE zebrarag_search_latency_ms histogram\n")
	cum := int64(0)
	for i, b := range searchLatencyBuckets {
		cum += atomic.LoadInt64(&searchLatencyCounts[i])
		fmt.Fprintf(w, "zebrarag_search_latency_ms_bucket{le=\"%d\"} %d\n", b, cum)
	}
	cum += atomic.LoadInt64(&searchLatencyCounts[len(searchLatencyBuckets)])
	fmt.Fprintf(w, "zebrarag_search_latency_ms_bucket{le=\"+Inf\"} %d\n", cum)
	fmt.Fprintf(w, "zebrarag_search_latency_ms_count %d\n", cum)
	fmt.Fprintf(w, "# HELP zebrarag_uptime_seconds Server uptime\n")
	fmt.Fprintf(w, "# TYPE zebrarag_uptime_seconds gauge\n")
	fmt.Fprintf(w, "zebrarag_uptime_seconds %d\n", int64(time.Since(startTime).Seconds()))
}
