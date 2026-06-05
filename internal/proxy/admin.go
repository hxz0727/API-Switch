package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/user/api-switch/internal/logutil"
	"github.com/user/api-switch/internal/monitor"
)

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	stats := s.tracker.Stats()
	recent := s.tracker.Recent(50)

	resp := map[string]interface{}{
		"stats":  stats,
		"recent": recent,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logutil.Warn("Failed to encode stats response: %v", err)
	}
}

func (s *Server) handleAdminEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cleanup := s.tracker.Subscribe(32)
	defer cleanup()

	// Send keepalive ping every 30s
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: request\ndata: %s\n\n", data)
			flusher.Flush()

		case <-ticker.C:
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

// handleAdminReload triggers a config reload from disk.
func (s *Server) handleAdminReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}

	s.reloadConfigFromFile()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Config reloaded: %d models, %d providers", len(s.cfg.Models), len(s.cfg.Providers)),
	}); err != nil {
		logutil.Warn("Failed to encode reload response: %v", err)
	}
}

// MonitorConnect connects to a running API-Switch SSE endpoint and prints live events.
func MonitorConnect(addr string) error {
	url := fmt.Sprintf("http://localhost%s/admin/events", addr)

	log.Printf("Connecting to API-Switch monitor at %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("cannot connect to API-Switch at %s: %w", addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	log.Println("Connected. Waiting for requests...")
	log.Println()
	log.Printf("  %-26s %-20s %-12s %s", "TIME", "MODEL", "PROVIDER", "STATUS")
	log.Printf("  " + strings.Repeat("─", 75))
	log.Println()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	var dataBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataBuf.WriteString(strings.TrimPrefix(line, "data: "))
		} else if line == "" && dataBuf.Len() > 0 {
			// Empty line = end of event
			if eventType == "request" {
				var ev monitor.RequestEvent
				if err := json.Unmarshal([]byte(dataBuf.String()), &ev); err == nil {
					printEvent(&ev)
				}
			}
			eventType = ""
			dataBuf.Reset()
		}
	}
	return scanner.Err()
}

func printEvent(ev *monitor.RequestEvent) {
	ts := ev.Timestamp.Format("15:04:05.000")
	durStr := formatDuration(ev.Duration)
	status := ev.Status
	if status == "ok" {
		status = "✓"
	} else if status == "error" {
		status = "✗ " + ev.Error
	} else {
		status = "~"
	}
	log.Printf("  %-26s %-20s %-12s %s (%s)", ts, ev.Model, ev.Provider, status, durStr)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

var dashboardHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>API-Switch Monitor</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#0d1117;color:#c9d1d9;padding:20px}
h1{font-size:20px;margin-bottom:16px;color:#58a6ff}
.stats{display:flex;gap:12px;margin-bottom:20px;flex-wrap:wrap}
.stat{background:#161b22;border:1px solid #30363d;border-radius:6px;padding:12px 16px;min-width:120px}
.stat-label{font-size:11px;color:#8b949e;text-transform:uppercase}
.stat-value{font-size:22px;font-weight:600;margin-top:4px}
table{width:100%;border-collapse:collapse;font-size:13px}
th{text-align:left;padding:8px 12px;border-bottom:2px solid #30363d;color:#8b949e;font-size:11px;text-transform:uppercase}
td{padding:8px 12px;border-bottom:1px solid #21262d}
tr:hover{background:#161b22}
.tag{display:inline-block;padding:1px 8px;border-radius:10px;font-size:11px;font-weight:500}
.tag-ok{background:#1b4b2e;color:#3fb950}
.tag-error{background:#4b1b1b;color:#f85149}
.tag-streaming{background:#1b2d4b;color:#58a6ff}
.tag-cancelled{background:#333;color:#8b949e}
.dur{font-variant-numeric:tabular-nums}
.model{color:#58a6ff}
.provider{color:#d2a8ff}
.time{color:#8b949e;font-size:12px}
@media(max-width:600px){td:nth-child(4),th:nth-child(4){display:none}}
</style></head>
<body>
<h1>API-Switch 实时监控</h1>
<div class=stats id=stats><div class=stat><div class=stat-label>请求总数</div><div class=stat-value id=total>-</div></div>
<div class=stat><div class=stat-label>平均耗时</div><div class=stat-value id=avg>-</div></div>
<div class=stat><div class=stat-label>模型数</div><div class=stat-value id=models>-</div></div></div>
<table><thead><tr><th>时间</th><th>模型</th><th>Provider</th><th>耗时</th><th>状态</th></tr></thead><tbody id=events></tbody></table>
<script>
const es=new EventSource('/admin/events');
es.addEventListener('request',e=>{
const d=JSON.parse(e.data);
const tbody=document.getElementById('events');
const ts=new Date(d.timestamp);
const time=ts.toLocaleTimeString();
const tagCls={ok:'tag-ok',error:'tag-error',streaming:'tag-streaming',cancelled:'tag-cancelled'}[d.status]||'tag-ok';
const dur=d.duration/1e6;
const durStr=dur<1000?Math.round(dur)+'ms':(dur/1e3).toFixed(1)+'s';
const row=document.createElement('tr');
row.innerHTML='<td class=time>'+time+'</td><td class=model>'+esc(d.model)+'</td><td class=provider>'+esc(d.provider)+'</td><td class=dur>'+durStr+'</td><td><span class="tag '+tagCls+'">'+d.status+'</span></td>';
tbody.prepend(row);
if(tbody.children.length>200)tbody.lastChild.remove();
updateStats(d);
});
function esc(s){const d=document.createElement('div');d.textContent=s;return d.innerHTML}
let reqCount=0,totalDur=0,modelSet=new Set;
function updateStats(d){reqCount++;totalDur+=d.duration/1e6;modelSet.add(d.model);
document.getElementById('total').textContent=reqCount;
document.getElementById('avg').textContent=reqCount?Math.round(totalDur/reqCount)+'ms':'-';
document.getElementById('models').textContent=modelSet.size;
}
</script></body></html>`
