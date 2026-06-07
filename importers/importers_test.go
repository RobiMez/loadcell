package importers

import "testing"

const k6NDJSON = `{"type":"Metric","data":{"name":"http_reqs","type":"counter"},"metric":"http_reqs"}
{"type":"Point","metric":"vus","data":{"time":"2026-06-05T10:00:00.0Z","value":5,"tags":{}}}
{"type":"Point","metric":"http_reqs","data":{"time":"2026-06-05T10:00:00.1Z","value":1,"tags":{"status":"200","method":"GET","url":"https://ex.com/","expected_response":"true","name":"https://ex.com/"}}}
{"type":"Point","metric":"http_req_duration","data":{"time":"2026-06-05T10:00:00.1Z","value":42.5,"tags":{"status":"200"}}}
{"type":"Point","metric":"http_reqs","data":{"time":"2026-06-05T10:00:00.5Z","value":1,"tags":{"status":"500","method":"GET","url":"https://ex.com/","expected_response":"false"}}}
{"type":"Point","metric":"http_req_duration","data":{"time":"2026-06-05T10:00:00.5Z","value":120.0,"tags":{"status":"500"}}}
{"type":"Point","metric":"http_reqs","data":{"time":"2026-06-05T10:00:01.2Z","value":1,"tags":{"status":"429","method":"GET","expected_response":"false"}}}
{"type":"Point","metric":"http_req_duration","data":{"time":"2026-06-05T10:00:01.2Z","value":15.0,"tags":{"status":"429"}}}
{"type":"Point","metric":"vus","data":{"time":"2026-06-05T10:00:01.2Z","value":8,"tags":{}}}`

func TestK6NDJSON(t *testing.T) {
	run, err := ImportRun("test.json", []byte(k6NDJSON))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if run.Metrics.TotalRequests != 3 {
		t.Errorf("total=%d want 3", run.Metrics.TotalRequests)
	}
	if run.Metrics.Successful != 1 {
		t.Errorf("successful=%d want 1", run.Metrics.Successful)
	}
	if run.Metrics.ServerErrors != 1 || run.Metrics.RateLimited != 1 {
		t.Errorf("server=%d rate=%d want 1/1", run.Metrics.ServerErrors, run.Metrics.RateLimited)
	}
	if run.Method != "GET" || run.URL != "https://ex.com/" {
		t.Errorf("method/url = %q %q", run.Method, run.URL)
	}
	if len(run.History) != 2 {
		t.Fatalf("history len=%d want 2 (two seconds)", len(run.History))
	}
	if run.History[0].TickRps != 2 || run.History[0].TickRpsOk != 1 {
		t.Errorf("sec0 rps=%v ok=%v want 2/1", run.History[0].TickRps, run.History[0].TickRpsOk)
	}
	if run.History[1].Conc != 8 {
		t.Errorf("sec1 conc=%d want 8", run.History[1].Conc)
	}
}

// Mirrors a real k6 handleSummary export: nested "thresholds" objects inside
// metrics, and the Rate metric http_req_failed where "value" is the failed
// fraction (here 0.05 → 50 failed) while passes/fails are inverted.
const k6SummaryJSON = `{"metrics":{
 "http_reqs":{"count":1000,"rate":100.0},
 "http_req_failed":{"passes":50,"fails":950,"value":0.05,"thresholds":{"rate<0.05":false}},
 "http_req_duration":{"avg":40,"min":5,"med":35,"max":300,"p(90)":80,"p(95)":120,"thresholds":{"p(95)<5000":true}}
}}`

func TestK6Summary(t *testing.T) {
	run, err := ImportRun("s.json", []byte(k6SummaryJSON))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if run.Metrics.TotalRequests != 1000 || run.Metrics.Successful != 950 {
		t.Errorf("total/ok = %d/%d want 1000/950", run.Metrics.TotalRequests, run.Metrics.Successful)
	}
	if run.Metrics.P50Ms != 35 || run.Metrics.P95Ms != 120 {
		t.Errorf("p50/p95 = %v/%v want 35/120", run.Metrics.P50Ms, run.Metrics.P95Ms)
	}
	if run.Metrics.ElapsedSecs != 10 {
		t.Errorf("elapsed=%v want 10", run.Metrics.ElapsedSecs)
	}
}

// A k6 summary reporting zero failures (passes:0, fails:850, value:0) must be
// read as 850 successful requests, not 850 failures.
const k6SummaryNoFail = `{"metrics":{
 "http_reqs":{"count":850,"rate":27.96},
 "http_req_failed":{"passes":0,"fails":850,"value":0,"thresholds":{"rate<0.05":false}},
 "http_req_duration":{"med":305.37,"max":4387.4,"p(90)":425.48,"p(95)":459.05,"avg":352.69,"min":266.12,"thresholds":{"p(95)<5000":false}}
}}`

func TestK6SummaryNoFailures(t *testing.T) {
	run, err := ImportRun("k6-summary.json", []byte(k6SummaryNoFail))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if run.Metrics.TotalRequests != 850 || run.Metrics.Successful != 850 {
		t.Errorf("total/ok = %d/%d want 850/850", run.Metrics.TotalRequests, run.Metrics.Successful)
	}
	if run.Metrics.Errors != 0 {
		t.Errorf("errors=%d want 0", run.Metrics.Errors)
	}
	if run.Metrics.P50Ms != 305.37 || run.Metrics.P95Ms != 459.05 {
		t.Errorf("p50/p95 = %v/%v", run.Metrics.P50Ms, run.Metrics.P95Ms)
	}
}

const vegetaJSON = `{
 "latencies":{"mean":40000000,"50th":35000000,"90th":80000000,"95th":120000000,"99th":250000000,"max":300000000},
 "duration":10000000000,
 "wait":1000000,
 "requests":1000,
 "rate":100.0,
 "throughput":99.0,
 "success":0.95,
 "status_codes":{"200":950,"500":40,"0":10},
 "errors":["dial tcp: timeout"]
}`

func TestVegeta(t *testing.T) {
	run, err := ImportRun("v.json", []byte(vegetaJSON))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if run.Metrics.TotalRequests != 1000 {
		t.Errorf("total=%d want 1000", run.Metrics.TotalRequests)
	}
	if run.Metrics.Successful != 950 || run.Metrics.ServerErrors != 40 || run.Metrics.NetworkErrors != 10 {
		t.Errorf("ok/5xx/net = %d/%d/%d want 950/40/10",
			run.Metrics.Successful, run.Metrics.ServerErrors, run.Metrics.NetworkErrors)
	}
	if run.Metrics.P50Ms != 35 || run.Metrics.P99Ms != 250 {
		t.Errorf("p50/p99 = %v/%v want 35/250 (ms)", run.Metrics.P50Ms, run.Metrics.P99Ms)
	}
	if run.Metrics.ElapsedSecs != 10 {
		t.Errorf("elapsed=%v want 10", run.Metrics.ElapsedSecs)
	}
}

// `vegeta encode -to=json` NDJSON: one result per line. Three requests
// over two seconds: one 200, one 500, one transport error (code=0).
const vegetaEncodedNDJSON = `{"attack":"","seq":0,"code":200,"timestamp":"2026-06-05T10:00:00.100Z","latency":12000000,"method":"GET","url":"https://ex.com/ok","error":""}
{"attack":"","seq":1,"code":500,"timestamp":"2026-06-05T10:00:00.500Z","latency":45000000,"method":"GET","url":"https://ex.com/ok","error":""}
{"attack":"","seq":2,"code":0,"timestamp":"2026-06-05T10:00:01.200Z","latency":1000000,"method":"GET","url":"https://ex.com/ok","error":"dial tcp: connection refused"}`

func TestVegetaEncodedNDJSON(t *testing.T) {
	run, err := ImportRun("results.json", []byte(vegetaEncodedNDJSON))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if run.Method != "GET" || run.URL != "https://ex.com/ok" {
		t.Errorf("method/url = %q %q", run.Method, run.URL)
	}
	if run.Metrics.TotalRequests != 3 {
		t.Errorf("total=%d want 3", run.Metrics.TotalRequests)
	}
	if run.Metrics.Successful != 1 || run.Metrics.ServerErrors != 1 || run.Metrics.NetworkErrors != 1 {
		t.Errorf("ok/5xx/net = %d/%d/%d want 1/1/1",
			run.Metrics.Successful, run.Metrics.ServerErrors, run.Metrics.NetworkErrors)
	}
	if len(run.History) != 2 {
		t.Fatalf("history len=%d want 2 (two seconds)", len(run.History))
	}
	if run.History[0].TickRps != 2 || run.History[0].TickRpsOk != 1 {
		t.Errorf("sec0 rps=%v ok=%v want 2/1", run.History[0].TickRps, run.History[0].TickRpsOk)
	}
	if run.History[1].TickRps != 1 {
		t.Errorf("sec1 rps=%v want 1", run.History[1].TickRps)
	}
	if run.Config.Mode != "imported" {
		t.Errorf("mode=%q want imported", run.Config.Mode)
	}
}

func TestUnknownFormat(t *testing.T) {
	if _, err := ImportRun("x", []byte(`{"foo":"bar"}`)); err == nil {
		t.Error("expected error for unknown format")
	}
}
