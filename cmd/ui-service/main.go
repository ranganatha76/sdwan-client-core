package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

// main starts the browser-facing UI and proxies its API calls to the control plane.
func main() {
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	if controlPlaneURL == "" {
		controlPlaneURL = "http://control-plane:8080"
	}
	target, err := url.Parse(controlPlaneURL)
	if err != nil {
		log.Fatalf("parse CONTROL_PLANE_URL: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	mux := http.NewServeMux()
	mux.Handle("/api/", proxy)
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /{$}", dashboard)

	server := &http.Server{Addr: ":8080", Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Println("UI service listening on :8080")
	log.Fatal(server.ListenAndServe())
}

// health reports that the browser-facing service can accept requests.
func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// dashboard serves the standalone UI; its JavaScript uses the proxied /api paths.
func dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page))
}

// securityHeaders prevents content-type sniffing and embedding from other origins.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

const page = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>PulseRoute</title><style>
@import url('https://fonts.googleapis.com/css2?family=DM+Mono:wght@400;500&family=Manrope:wght@400;600;700;800&display=swap');
:root{--ink:#112e30;--muted:#657c78;--line:#d9e5e0;--bg:#f4f7f4;--paper:#fffefa;--teal:#087b70;--deep:#123d3d;--mint:#d9f1e7;--lime:#b9e45c;--amber:#e9ae3c}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:14px Manrope,sans-serif}.shell{max-width:1180px;margin:auto;padding:30px 25px 50px}.bar{display:flex;justify-content:space-between;align-items:flex-start;gap:20px;margin-bottom:22px}.brand{display:flex;gap:10px;align-items:center;font-weight:800;font-size:19px}.mark{display:grid;place-items:center;width:32px;height:32px;background:var(--deep);color:var(--lime);border-radius:7px;font:13px 'DM Mono',monospace}.eyebrow{font:700 10px 'DM Mono',monospace;letter-spacing:1px;color:#4f7770;margin:0 0 8px;text-transform:uppercase}h1{font-size:27px;margin:0 0 4px;letter-spacing:0}.sub{margin:0;color:var(--muted);font-size:12px}.connection{background:var(--paper);border:1px solid var(--line);border-radius:7px;padding:10px 13px;font-size:12px}.connection strong{display:block}.connection span{color:#4a796e;font-size:10px}.notice{display:flex;justify-content:space-between;gap:15px;background:var(--mint);border:1px solid #c0dfd4;border-radius:7px;padding:13px 15px;margin-bottom:19px}.notice b{font-size:12px}.notice p{margin:3px 0 0;font-size:11px;color:#4b756c}.grid{display:grid;grid-template-columns:1.4fr .8fr;gap:18px}.panel{background:var(--paper);border:1px solid var(--line);border-radius:7px;overflow:hidden}.head{padding:16px 17px;border-bottom:1px solid var(--line);display:flex;justify-content:space-between;align-items:center}.head h2{margin:0;font-size:14px}.head p{margin:3px 0 0;color:var(--muted);font-size:10px}.button{border:0;border-radius:5px;background:var(--teal);color:#fff;padding:8px 10px;font:700 10px Manrope,sans-serif;cursor:pointer}.button:hover{background:#05675f}.apps{padding:0 17px}.app{display:grid;grid-template-columns:1fr 105px 80px;gap:10px;align-items:center;padding:13px 0;border-bottom:1px solid #ebf0ed}.app:last-child{border:0}.app-name{font-size:12px;font-weight:700}.category{color:var(--muted);font-size:10px;margin-top:2px}.route{color:#32645c;font-size:10px}.status{font:10px 'DM Mono',monospace;color:#33796d}.status:before{content:'';display:inline-block;width:6px;height:6px;background:var(--lime);border-radius:50%;margin-right:5px}.paths{padding:0 17px}.path{padding:13px 0;border-bottom:1px solid #ebf0ed}.path:last-child{border:0}.path b{font-size:11px}.best{color:#147064;font:10px 'DM Mono',monospace;margin-left:6px}.metrics{display:flex;gap:13px;margin-top:4px;color:var(--muted);font:10px 'DM Mono',monospace}.activity{margin-top:18px}.event{padding:11px 0;border-bottom:1px solid #ebf0ed;font-size:10px}.event:last-child{border:0}.event time{float:right;color:var(--muted);font-family:'DM Mono',monospace}@media(max-width:750px){.bar{display:block}.connection{margin-top:14px;width:max-content}.grid{grid-template-columns:1fr}.app{grid-template-columns:1fr 80px}.route{display:none}}@media(max-width:430px){.shell{padding:20px 14px}.notice{display:block}.button{margin-top:10px}h1{font-size:22px}}
</style></head><body><main class="shell"><header class="bar"><div><div class="brand"><span class="mark">PR</span>PulseRoute</div><p class="eyebrow" style="margin-top:25px">Remote performance console</p><h1>Work traffic gets the best path.</h1><p class="sub">Live routing policy for <span id="location">your device</span></p></div><div class="connection"><strong id="connection">Protection active</strong><span>Control plane connected</span></div></header><section class="notice"><div><b>Route intelligence is active.</b><p>Priority apps use the highest-quality available route.</p></div><button class="button" id="enable">Enable all apps</button></section><section class="grid"><article class="panel"><div class="head"><div><h2>Application policies</h2><p>Business applications protected by PulseRoute.</p></div><button class="button" id="optimize">Optimize routes</button></div><div class="apps" id="apps">Loading policies...</div></article><div><article class="panel"><div class="head"><div><h2>Available paths</h2><p>Measured <span id="paths-measured-at">now</span>; refreshes every 30 seconds</p></div></div><div class="paths" id="paths"></div></article><article class="panel activity"><div class="head"><div><h2>Routing activity</h2><p>Latest decisions</p></div></div><div class="paths" id="events"></div></article></div></section></main><script>
const esc=v=>String(v).replace(/[&<>'"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));async function call(url,opts){const r=await fetch(url,opts);if(!r.ok)throw Error('Request failed');return r.json()}function draw(s){location.textContent=s.location;connection.textContent=s.connected?'Protection active':'Protection paused';document.getElementById('paths-measured-at').textContent=s.pathsMeasuredAt;apps.innerHTML=s.apps.map(a=>'<div class="app"><div><div class="app-name">'+esc(a.name)+'</div><div class="category">'+esc(a.category)+'</div></div><div class="route">'+esc(a.route)+'</div><div class="status">'+esc(a.priority)+'</div></div>').join('');paths.innerHTML=s.paths.map(p=>'<div class="path"><b>'+esc(p.name)+(p.selected?' <span class="best">BEST</span>':'')+'</b><div class="metrics"><span>'+p.latency+' ms</span><span>'+p.loss+' loss</span><span>'+p.jitter+' jitter</span></div></div>').join('');events.innerHTML=s.activity.map(e=>'<div class="event">'+esc(e.message)+'<time>'+esc(e.time)+'</time></div>').join('')}async function refresh(){try{draw(await call('/api/state'))}catch(error){apps.textContent='Control plane unavailable'}}document.getElementById('enable').onclick=async()=>{draw(await call('/api/policies/enable-all',{method:'POST'}))};document.getElementById('optimize').onclick=async()=>{draw(await call('/api/optimize',{method:'POST'}))};refresh();setInterval(refresh,1000);
</script></body></html>`
