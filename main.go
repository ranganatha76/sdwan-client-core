package main

import (
	"encoding/json"
	"log"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

type application struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Priority string `json:"priority"`
	Route    string `json:"route"`
	Health   string `json:"health"`
	Active   bool   `json:"active"`
}

type path struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Latency  int    `json:"latency"`
	Loss     string `json:"loss"`
	Jitter   int    `json:"jitter"`
	Score    int    `json:"score"`
	Selected bool   `json:"selected"`
}

type activity struct {
	Time    string `json:"time"`
	Message string `json:"message"`
	Kind    string `json:"kind"`
}

type clientState struct {
	Connected bool          `json:"connected"`
	Location  string        `json:"location"`
	Apps      []application `json:"apps"`
	Paths     []path        `json:"paths"`
	Activity  []activity    `json:"activity"`
}

type server struct {
	mu    sync.RWMutex
	state clientState
}

func main() {
	s := &server{state: initialState()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.getState)
	mux.HandleFunc("POST /api/policies/", s.togglePolicy)
	mux.HandleFunc("POST /api/optimize", s.optimize)
	mux.HandleFunc("POST /api/connection", s.toggleConnection)
	mux.HandleFunc("GET /", s.dashboard)

	log.Println("SD-WAN client listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func initialState() clientState {
	return clientState{
		Connected: true,
		Location:  "Seattle, WA",
		Apps: []application{
			{ID: "zoom", Name: "Zoom", Category: "Video collaboration", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "salesforce", Name: "Salesforce", Category: "Customer operations", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "teams", Name: "Microsoft Teams", Category: "Video collaboration", Priority: "High", Route: "LTE failover", Health: "Protected", Active: true},
			{ID: "slack", Name: "Slack", Category: "Messaging", Priority: "High", Route: "Fiber primary", Health: "Protected", Active: true},
			{ID: "github", Name: "GitHub", Category: "Engineering", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "bitbucket", Name: "Bitbucket", Category: "Engineering", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "microsoft-365", Name: "Microsoft 365", Category: "Productivity", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "google-workspace", Name: "Google Workspace", Category: "Productivity", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "jira", Name: "Jira", Category: "Productivity", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "asana", Name: "Asana", Category: "Productivity", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "notion", Name: "Notion", Category: "Productivity", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
			{ID: "figma", Name: "Figma", Category: "Productivity", Priority: "High", Route: "Fiber primary", Health: "Optimal", Active: true},
		},
		Paths: []path{
			{Name: "Fiber primary", Provider: "Home ISP", Latency: 18, Loss: "0.02%", Jitter: 3, Score: 98, Selected: true},
			{Name: "LTE failover", Provider: "eSIM cellular", Latency: 42, Loss: "0.11%", Jitter: 8, Score: 84},
			{Name: "Direct internet", Provider: "Home ISP", Latency: 26, Loss: "0.08%", Jitter: 5, Score: 91},
		},
		Activity: []activity{
			{Time: "Just now", Message: "Zoom pinned to Fiber primary", Kind: "success"},
			{Time: "08:41", Message: "Work application policy loaded", Kind: "neutral"},
			{Time: "08:32", Message: "Teams protected by LTE failover", Kind: "warning"},
		},
	}
}

func (s *server) getState(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.state)
}

func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page))
}

func (s *server) togglePolicy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/policies/"), "/toggle")
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.state.Apps {
		app := &s.state.Apps[index]
		if app.ID != id {
			continue
		}
		app.Active = !app.Active
		if app.Active {
			app.Priority = "High"
			app.Health, app.Route = "Protected", selectedPath(s.state.Paths).Name
		} else {
			app.Priority = "Standard"
			app.Health, app.Route = "Standard", "Direct internet"
		}
		s.addActivity("Policy updated for "+app.Name, "success")
		writeJSON(w, app)
		return
	}
	http.NotFound(w, r)
}

func (s *server) optimize(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bestIndex := 0
	for index := range s.state.Paths {
		current := &s.state.Paths[index]
		current.Latency = max(12, current.Latency+rand.IntN(9)-4)
		current.Jitter = max(2, current.Jitter+rand.IntN(5)-2)
		current.Score = min(99, max(72, 100-current.Latency/2-current.Jitter))
		if current.Score > s.state.Paths[bestIndex].Score {
			bestIndex = index
		}
	}
	for index := range s.state.Paths {
		s.state.Paths[index].Selected = index == bestIndex
	}
	for index := range s.state.Apps {
		if s.state.Apps[index].Active {
			s.state.Apps[index].Route = s.state.Paths[bestIndex].Name
		}
	}
	s.addActivity("Route optimization completed: "+s.state.Paths[bestIndex].Name+" selected", "success")
	writeJSON(w, s.state)
}

func (s *server) toggleConnection(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Connected = !s.state.Connected
	if s.state.Connected {
		s.addActivity("SD-WAN protection enabled", "success")
	} else {
		s.addActivity("SD-WAN protection paused", "warning")
	}
	writeJSON(w, s.state)
}

func (s *server) addActivity(message, kind string) {
	s.state.Activity = append([]activity{{Time: time.Now().Format("15:04"), Message: message, Kind: kind}}, s.state.Activity...)
	if len(s.state.Activity) > 4 {
		s.state.Activity = s.state.Activity[:4]
	}
}

func selectedPath(paths []path) path {
	for _, path := range paths {
		if path.Selected {
			return path
		}
	}
	return paths[0]
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

const page = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>PulseRoute</title>
<style>
@import url('https://fonts.googleapis.com/css2?family=DM+Mono:wght@400;500&family=Manrope:wght@400;500;600;700;800&display=swap');
:root{--ink:#102b2e;--muted:#6a7e7d;--line:#dce5e1;--canvas:#f5f7f3;--paper:#fffefa;--teal:#0e8076;--teal-dark:#075b54;--mint:#d9f1e8;--lime:#b9e45c;--amber:#efb748}*{box-sizing:border-box}body{margin:0;background:var(--canvas);color:var(--ink);font:14px Manrope,sans-serif}.app{min-height:100vh;display:grid;grid-template-columns:250px 1fr}aside{background:#123a3c;color:#e9f3ee;padding:28px 18px;display:flex;flex-direction:column}.brand{display:flex;gap:10px;align-items:center;font-weight:800;font-size:18px;margin:0 8px 42px}.brandmark{display:grid;place-items:center;width:30px;height:30px;background:var(--lime);color:#173437;border-radius:7px;font:15px 'DM Mono',monospace}.eyebrow{color:#a9c6be;font-size:10px;font-weight:700;letter-spacing:1.2px;margin:0 8px 10px;text-transform:uppercase}.nav{display:grid;gap:4px}.nav a{color:#bed3cd;padding:10px 11px;text-decoration:none;border-radius:6px;font-size:13px}.nav a.active{background:#275457;color:#fff;font-weight:700}.nav a span{display:inline-block;width:22px;color:var(--lime);font-family:'DM Mono',monospace}.foot{margin-top:auto;padding:15px 10px 0;border-top:1px solid #356165;color:#b5c9c3;font-size:11px;line-height:1.7}.dot{width:7px;height:7px;border-radius:50%;display:inline-block;background:var(--lime);margin-right:6px}main{padding:34px 40px 42px;max-width:1460px;width:100%;margin:auto}.topbar{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:31px}h1{font-size:27px;margin:3px 0 4px}.subtitle{margin:0;color:var(--muted);font-size:13px}.connection{display:flex;align-items:center;gap:12px;background:var(--paper);border:1px solid var(--line);padding:10px 12px 10px 14px;border-radius:7px}.connection strong{display:block;font-size:12px}.connection small{display:block;color:var(--muted);font-size:10px;margin-top:2px}.switch{width:39px;height:22px;background:#9aa9a5;border:0;border-radius:12px;padding:3px}.switch i{display:block;width:16px;height:16px;background:#fff;border-radius:50%;transition:.2s}.switch.on{background:var(--teal)}.switch.on i{transform:translateX(17px)}.banner{background:var(--mint);border:1px solid #bfe0d2;border-radius:7px;padding:13px 16px;display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:25px}.banner b{font-size:13px}.banner p{margin:3px 0 0;color:#477168;font-size:12px}.tag{font:10px 'DM Mono',monospace;color:#19695f;white-space:nowrap}.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-bottom:25px}.stat{background:var(--paper);border:1px solid var(--line);padding:16px;border-radius:7px;min-height:104px}.stat-label{color:var(--muted);font-size:11px;font-weight:700}.stat-value{margin:12px 0 3px;font:500 25px 'DM Mono',monospace}.stat-note{color:#54867a;font-size:10px}.content{display:grid;grid-template-columns:minmax(0,1.55fr) minmax(280px,.8fr);gap:20px}.panel{background:var(--paper);border:1px solid var(--line);border-radius:7px;overflow:hidden}.panel-head{padding:18px 19px 14px;display:flex;justify-content:space-between;align-items:flex-start;border-bottom:1px solid var(--line)}h2{margin:0;font-size:15px}.panel-head p{margin:4px 0 0;color:var(--muted);font-size:11px}.btn{border:1px solid var(--teal);border-radius:5px;background:var(--teal);color:#fff;padding:8px 10px;font-size:11px;font-weight:700}.btn:hover{background:var(--teal-dark)}.app-list{padding:0 18px}.app-row{display:grid;grid-template-columns:1.1fr .9fr .8fr 74px;gap:10px;align-items:center;padding:14px 0;border-bottom:1px solid #edf1ee}.app-row:last-child,.path:last-child,.event:last-child{border:0}.app-name{display:flex;align-items:center;gap:9px;font-weight:700;font-size:12px}.app-icon{width:27px;height:27px;display:grid;place-items:center;color:#fff;font:10px 'DM Mono',monospace;border-radius:5px;background:#2573b8}.app-icon.salesforce{background:#2074b2}.app-icon.teams{background:#6356a5}.app-icon.github{background:#26343b}.app-icon.slack{background:#bd654e}.app-category{color:var(--muted);font-size:10px;margin-top:2px}.route{font-size:11px;color:#315e59}.health{font-size:10px;color:#39776b}.health:before{content:'';display:inline-block;background:var(--lime);border-radius:50%;width:6px;height:6px;margin-right:5px}.health.standard{color:#7a8885}.health.standard:before{background:#acb8b2}.row-switch{justify-self:end}.paths{padding:7px 18px 14px}.path{display:grid;grid-template-columns:1.2fr repeat(3,.65fr);gap:8px;align-items:center;padding:14px 0;border-bottom:1px solid #edf1ee}.path.selected{background:#f2faf4;margin:0 -8px;padding:14px 8px}.path-name{font-size:12px;font-weight:700}.path-provider{color:var(--muted);font-size:10px;margin-top:3px}.metric{font:11px 'DM Mono',monospace;color:#345c59}.metric small{color:var(--muted);font:9px Manrope,sans-serif}.activity{margin-top:20px}.activity-list{padding:4px 18px 12px}.event{display:grid;grid-template-columns:8px 1fr auto;gap:9px;align-items:center;padding:12px 0;border-bottom:1px solid #edf1ee;font-size:11px}.event-dot{width:7px;height:7px;border-radius:50%;background:var(--teal)}.event-dot.warning{background:var(--amber)}.event-dot.neutral{background:#9caeaa}.event time{color:var(--muted);font:10px 'DM Mono',monospace}@media(max-width:900px){.app{grid-template-columns:1fr}aside{padding:16px 20px;flex-direction:row;align-items:center}.brand{margin:0 auto 0 0}.nav{display:flex;gap:3px}.nav a{padding:6px;font-size:0}.nav a span{font-size:12px}.foot{display:none}main{padding:25px 20px}.stats{grid-template-columns:repeat(2,1fr)}.content{grid-template-columns:1fr}}@media(max-width:570px){.topbar{display:block}.connection{margin-top:18px;width:max-content}.banner{display:block}.tag{display:block;margin-top:9px}.stats{gap:8px}.stat{padding:12px;min-height:90px}.stat-value{font-size:20px}.app-row{grid-template-columns:1fr 72px}.app-row>.route,.app-row>.health{display:none}.path{grid-template-columns:1fr repeat(2,.65fr)}.path .jitter{display:none}}
</style></head><body><div class="app"><aside><div class="brand"><span class="brandmark">PR</span> PulseRoute</div><div><p class="eyebrow">Workspace</p><nav class="nav"><a class="active" href="#overview"><span>01</span>Overview</a><a href="#policies"><span>02</span>Policies</a><a href="#paths"><span>03</span>Path health</a><a href="#activity"><span>04</span>Activity</a></nav></div><div class="foot"><span class="dot"></span><span id="side-status">Client protected</span><br>v0.1.0 beta<br>Device: RD-MBP-14</div></aside><main id="overview"><header class="topbar"><div><p class="eyebrow" style="color:#4e7771;margin:0">Remote performance console</p><h1>Network that works around home internet.</h1><p class="subtitle">Active routing policy for <span id="location">Seattle, WA</span></p></div><div class="connection"><div><strong id="connection-label">Protection active</strong><small>Business traffic is prioritized</small></div><button class="switch on" id="connection-toggle" aria-label="Toggle connection"><i></i></button></div></header><section class="banner"><div><b>Route intelligence is active.</b><p>Critical applications are continuously evaluated against available paths.</p></div><span class="tag">SCAN: 30 SEC</span></section><section class="stats"><article class="stat"><div class="stat-label">PROTECTED APPS</div><div class="stat-value" id="protected-count">04</div><div class="stat-note">Routing policy enabled</div></article><article class="stat"><div class="stat-label">BEST PATH LATENCY</div><div class="stat-value" id="best-latency">18ms</div><div class="stat-note">Fiber primary</div></article><article class="stat"><div class="stat-label">TRAFFIC STEERED</div><div class="stat-value">1.8 GB</div><div class="stat-note">Since 08:00 today</div></article><article class="stat"><div class="stat-label">CALL QUALITY SCORE</div><div class="stat-value">4.8/5</div><div class="stat-note">Excellent experience</div></article></section><section class="content"><div><article class="panel" id="policies"><div class="panel-head"><div><h2>Application policies</h2><p>Priority traffic uses the best available route.</p></div><button class="btn" id="optimize">Optimize routes</button></div><div class="app-list" id="app-list"></div></article></div><div><article class="panel" id="paths"><div class="panel-head"><div><h2>Available paths</h2><p>Real-time link assessment</p></div></div><div class="paths" id="paths-list"></div></article><article class="panel activity" id="activity"><div class="panel-head"><div><h2>Routing activity</h2><p>Latest policy decisions</p></div></div><div class="activity-list" id="activity-list"></div></article></div></section></main></div><script>
const e=v=>String(v).replace(/[&<>'"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));const initials=n=>n.split(' ').map(w=>w[0]).join('').slice(0,2);async function req(u,o){const r=await fetch(u,o);if(!r.ok)throw Error('Request failed');return r.json()}function render(d){location.textContent=d.location;const on=d.apps.filter(a=>a.active).length;document.querySelector('#protected-count').textContent=String(on).padStart(2,'0');const best=d.paths.find(p=>p.selected)||d.paths[0];document.querySelector('#best-latency').textContent=best.latency+'ms';document.querySelector('#app-list').innerHTML=d.apps.map(a=>'<div class="app-row"><div><div class="app-name"><span class="app-icon '+a.id+'">'+initials(a.name)+'</span>'+e(a.name)+'</div><div class="app-category">'+e(a.category)+'</div></div><div class="route">'+e(a.route)+'</div><div class="health '+(a.active?'':'standard')+'">'+e(a.health)+'</div><button class="switch row-switch '+(a.active?'on':'')+'" data-policy="'+a.id+'" aria-label="Toggle '+e(a.name)+'"><i></i></button></div>').join('');document.querySelector('#paths-list').innerHTML=d.paths.map(p=>'<div class="path '+(p.selected?'selected':'')+'"><div><div class="path-name">'+e(p.name)+(p.selected?' <span class="tag">BEST</span>':'')+'</div><div class="path-provider">'+e(p.provider)+'</div></div><div class="metric">'+p.latency+' <small>ms</small></div><div class="metric">'+p.loss+' <small>loss</small></div><div class="metric jitter">'+p.jitter+' <small>jitter</small></div></div>').join('');document.querySelector('#activity-list').innerHTML=d.activity.map(a=>'<div class="event"><span class="event-dot '+a.kind+'"></span><span>'+e(a.message)+'</span><time>'+e(a.time)+'</time></div>').join('');connectionToggle.classList.toggle('on',d.connected);connectionLabel.textContent=d.connected?'Protection active':'Protection paused';sideStatus.textContent=d.connected?'Client protected':'Client paused'}async function refresh(){render(await req('/api/state'))}document.addEventListener('click',async x=>{const p=x.target.closest('[data-policy]');if(p)render(await req('/api/policies/'+p.dataset.policy+'/toggle',{method:'POST'}));if(x.target.closest('#optimize'))render(await req('/api/optimize',{method:'POST'}));if(x.target.closest('#connection-toggle'))render(await req('/api/connection',{method:'POST'}))});refresh();
</script></body></html>`