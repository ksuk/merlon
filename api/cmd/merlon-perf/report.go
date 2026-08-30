package main

import (
	"math"
	"sort"
	"time"
)

type requestResult struct {
	duration       time.Duration
	statusCode     int
	transportError bool
}

type latencySummary struct {
	Min float64 `json:"min"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

type resultSummary struct {
	Attempted               int            `json:"attempted"`
	Succeeded               int            `json:"succeeded"`
	Failed                  int            `json:"failed"`
	TransportErrors         int            `json:"transport_errors"`
	StatusCodes             map[int]int    `json:"status_codes"`
	ErrorRatePercent        float64        `json:"error_rate_percent"`
	SuccessfulThroughputRPS float64        `json:"successful_throughput_rps"`
	SuccessfulLatencyMS     latencySummary `json:"successful_latency_ms"`
}

func summarizeResults(started, completed time.Time, results []requestResult) resultSummary {
	summary := resultSummary{
		Attempted:   len(results),
		StatusCodes: map[int]int{},
	}
	successfulDurations := make([]time.Duration, 0, len(results))
	for _, result := range results {
		if result.transportError {
			summary.TransportErrors++
			summary.Failed++
			continue
		}
		summary.StatusCodes[result.statusCode]++
		if result.statusCode >= 200 && result.statusCode < 300 {
			summary.Succeeded++
			successfulDurations = append(successfulDurations, result.duration)
		} else {
			summary.Failed++
		}
	}
	if summary.Attempted > 0 {
		summary.ErrorRatePercent = round3(float64(summary.Failed) * 100 / float64(summary.Attempted))
	}
	if elapsed := completed.Sub(started).Seconds(); elapsed > 0 {
		summary.SuccessfulThroughputRPS = round3(float64(summary.Succeeded) / elapsed)
	}
	summary.SuccessfulLatencyMS = summarizeLatency(successfulDurations)
	return summary
}

func summarizeLatency(durations []time.Duration) latencySummary {
	if len(durations) == 0 {
		return latencySummary{}
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return latencySummary{
		Min: durationMilliseconds(sorted[0]),
		P50: durationMilliseconds(nearestRank(sorted, 0.50)),
		P95: durationMilliseconds(nearestRank(sorted, 0.95)),
		P99: durationMilliseconds(nearestRank(sorted, 0.99)),
		Max: durationMilliseconds(sorted[len(sorted)-1]),
	}
}

func nearestRank(sorted []time.Duration, percentile float64) time.Duration {
	rank := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func durationMilliseconds(duration time.Duration) float64 {
	return round3(float64(duration) / float64(time.Millisecond))
}

func round3(value float64) float64 {
	return math.Round(value*1000) / 1000
}
