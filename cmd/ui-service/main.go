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

	server := &http.Server{
		Addr:              ":8080",
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Println("UI service listening on :8080")
	log.Fatal(server.ListenAndServe())
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>PulseRoute</title>
  <style>
    @import url('https://fonts.googleapis.com/css2?family=DM+Mono:wght@400;500&family=Manrope:wght@400;600;700;800&display=swap');
    :root { --ink:#123033; --muted:#637b77; --line:#d9e5e0; --canvas:#f3f7f4; --paper:#fffefa; --teal:#087b70; --teal-dark:#05665e; --mint:#d9f1e7; --lime:#b9e45c; --off:#a9b8b4; }
    * { box-sizing:border-box; }
    body { margin:0; background:var(--canvas); color:var(--ink); font:13px Manrope,sans-serif; }
    .shell { max-width:1160px; margin:auto; padding:28px 22px 48px; }
    .bar, .head, .app { display:flex; align-items:center; }
    .bar, .head { justify-content:space-between; gap:16px; }
    .bar { align-items:flex-start; margin-bottom:22px; }
    .brand { display:flex; gap:10px; align-items:center; font-size:18px; font-weight:800; }
    .mark { display:grid; place-items:center; width:31px; height:31px; border-radius:7px; background:#123d3d; color:var(--lime); font:12px 'DM Mono',monospace; }
    .eyebrow { margin:22px 0 6px; color:#4d766e; font:700 10px 'DM Mono',monospace; letter-spacing:1px; text-transform:uppercase; }
    h1 { margin:0; font-size:27px; }
    .sub { margin:4px 0 0; color:var(--muted); font-size:12px; }
    .connection { border:1px solid var(--line); border-radius:7px; background:var(--paper); padding:10px 12px; font-size:12px; }
    .connection strong, .connection span { display:block; }
    .connection span { margin-top:2px; color:#4a796e; font-size:10px; }
    .notice { display:flex; justify-content:space-between; align-items:center; gap:14px; margin-bottom:18px; padding:13px 15px; border:1px solid #c1dfd5; border-radius:7px; background:var(--mint); }
    .notice b { font-size:12px; }
    .notice p, .head p { margin:3px 0 0; color:var(--muted); font-size:10px; }
    .layout { display:grid; grid-template-columns:minmax(0,1.4fr) minmax(290px,.8fr); gap:18px; }
    .panel { overflow:hidden; border:1px solid var(--line); border-radius:7px; background:var(--paper); }
    .head { padding:15px 16px; border-bottom:1px solid var(--line); }
    h2 { margin:0; font-size:14px; }
    .button { border:0; border-radius:5px; background:var(--teal); color:#fff; padding:8px 10px; font:700 10px Manrope,sans-serif; cursor:pointer; }
    .button:hover { background:var(--teal-dark); }
    .button:disabled, .switch:disabled { cursor:wait; opacity:.55; }
    .apps, .paths, .events { padding:0 16px; }
    .app { display:grid; grid-template-columns:minmax(180px,1fr) 110px 82px 74px; gap:9px; padding:12px 0; border-bottom:1px solid #ebf0ed; }
    .app:last-child, .path:last-child, .event:last-child { border-bottom:0; }
    .app-name { font-weight:800; font-size:12px; }
    .category, .route, .status { margin-top:2px; color:var(--muted); font-size:10px; }
    .status { color:#458076; }
    .status.off { color:#778783; }
    .switch { position:relative; width:42px; height:23px; border:0; border-radius:12px; background:var(--off); padding:3px; cursor:pointer; }
    .switch span { display:block; width:17px; height:17px; border-radius:50%; background:#fff; transition:transform .15s; }
    .switch.on { background:var(--teal); }
    .switch.on span { transform:translateX(19px); }
    .control-label { font-size:10px; color:var(--muted); text-align:right; }
    .path, .event { padding:13px 0; border-bottom:1px solid #ebf0ed; }
    .path-top, .metrics { display:flex; justify-content:space-between; gap:9px; }
    .path b { font-size:12px; }
    .best { margin-left:4px; color:#37766d; font:700 9px 'DM Mono',monospace; }
    .metrics { margin-top:4px; color:#4d7770; font:10px 'DM Mono',monospace; }
    .activity { margin-top:16px; }
    .event { display:flex; gap:8px; color:#415f59; font-size:10px; }
    .event:before { content:''; width:5px; height:5px; flex:0 0 5px; margin-top:4px; border-radius:50%; background:var(--teal); }
    .event time { margin-left:auto; color:#6f8580; font-family:'DM Mono',monospace; }
    .error { color:#9d3c34; }
    @media (max-width:760px) { .shell { padding:20px 14px; } .bar { display:block; } .connection { display:inline-block; margin-top:16px; } .layout { grid-template-columns:1fr; } .app { grid-template-columns:minmax(130px,1fr) 76px 60px 48px; } .route { white-space:nowrap; overflow:hidden; text-overflow:ellipsis; } .control-label { display:none; } }
  </style>
</head>
<body>
  <main class="shell">
    <header class="bar">
      <div>
        <div class="brand"><span class="mark">PR</span>PulseRoute</div>
        <p class="eyebrow">Remote performance console</p>
        <h1>Work traffic gets the best path.</h1>
        <p class="sub">Live routing policy for <span id="location-label">your device</span></p>
      </div>
      <div class="connection"><strong id="connection-label">Protection active</strong><span>Control plane connected</span></div>
    </header>
    <section class="notice">
      <div><b>Route intelligence is active.</b><p>Enable or disable each business app independently.</p></div>
      <button class="button" id="enable-all" type="button">Enable all apps</button>
    </section>
    <section class="layout">
      <article class="panel">
        <div class="head"><div><h2>Application policies</h2><p>Individual routing controls</p></div><button class="button" id="optimize" type="button">Optimize routes</button></div>
        <div class="apps" id="apps">Loading policies...</div>
      </article>
      <div>
        <article class="panel">
          <div class="head"><div><h2>Available paths</h2><p id="paths-measured-at">Awaiting measurement</p></div><button class="button" id="refresh-paths" type="button">Refresh paths</button></div>
          <div class="paths" id="paths"></div>
        </article>
        <article class="panel activity">
          <div class="head"><div><h2>Routing activity</h2><p>Latest decisions</p></div></div>
          <div class="events" id="events"></div>
        </article>
      </div>
    </section>
  </main>
  <script>
    const byID = id => document.getElementById(id);
    const elements = {
      apps: byID('apps'),
      connectionLabel: byID('connection-label'),
      events: byID('events'),
      locationLabel: byID('location-label'),
      paths: byID('paths'),
      pathsMeasuredAt: byID('paths-measured-at'),
    };
    const escapeHTML = value => String(value).replace(/[&<>'"]/g, character => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', "'":'&#39;', '"':'&quot;' }[character]));
    let lastPathCheck;

    async function call(url, options) {
      const response = await fetch(url, options);
      if (!response.ok) throw Error('Request failed');
      return response.json();
    }

    function draw(state) {
      elements.locationLabel.textContent = state.location;
      elements.connectionLabel.textContent = state.connected ? 'Protection active' : 'Protection paused';
      elements.apps.innerHTML = state.apps.map(app => '<div class="app"><div><div class="app-name">'+escapeHTML(app.name)+'</div><div class="category">'+escapeHTML(app.category)+'</div></div><div class="route">'+escapeHTML(app.route)+'</div><div class="status '+(app.active ? '' : 'off')+'">'+(app.active ? 'Enabled' : 'Disabled')+'</div><div><button class="switch '+(app.active ? 'on' : '')+'" data-policy="'+escapeHTML(app.id)+'" aria-label="'+(app.active ? 'Disable ' : 'Enable ')+escapeHTML(app.name)+'" aria-pressed="'+app.active+'"><span></span></button><div class="control-label">'+(app.active ? 'On' : 'Off')+'</div></div></div>').join('');
      const checkedAt = lastPathCheck ? 'Checked '+lastPathCheck.toLocaleTimeString() : 'Not checked yet';
      elements.pathsMeasuredAt.textContent = state.pathsMeasuredAt === 'Awaiting first probe' ? checkedAt+'; awaiting first probe' : 'Telemetry '+state.pathsMeasuredAt+'; '+checkedAt;
      elements.paths.innerHTML = state.paths.map(path => '<div class="path"><div class="path-top"><b>'+escapeHTML(path.name)+(path.selected ? ' <span class="best">BEST</span>' : '')+'</b><span class="route">'+escapeHTML(path.provider)+'</span></div><div class="metrics"><span>'+path.latency+' ms</span><span>'+escapeHTML(path.loss)+' loss</span><span>'+path.jitter+' jitter</span></div></div>').join('');
      elements.events.innerHTML = state.activity.map(event => '<div class="event"><span>'+escapeHTML(event.message)+'</span><time>'+escapeHTML(event.time)+'</time></div>').join('');
    }

    async function refresh(button) {
      if (button) button.disabled = true;
      try {
        const state = await call('/api/state');
        lastPathCheck = new Date();
        draw(state);
      } catch (error) {
        elements.apps.innerHTML = '<p class="error">Control plane unavailable</p>';
      } finally {
        if (button) button.disabled = false;
      }
    }

    async function act(button, url) {
      button.disabled = true;
      try {
        await call(url, { method: 'POST' });
        await refresh();
      } catch (error) {
        elements.apps.innerHTML = '<p class="error">The request could not be completed.</p>';
      } finally {
        button.disabled = false;
      }
    }

    document.addEventListener('click', event => {
      const policyButton = event.target.closest('[data-policy]');
      if (policyButton) return act(policyButton, '/api/policies/'+policyButton.dataset.policy+'/toggle');
      if (event.target.closest('#refresh-paths')) return refresh(event.target.closest('#refresh-paths'));
      if (event.target.closest('#enable-all')) return act(event.target.closest('#enable-all'), '/api/policies/enable-all');
      if (event.target.closest('#optimize')) return act(event.target.closest('#optimize'), '/api/optimize');
    });

    refresh();
    setInterval(() => refresh(), 30000);
  </script>
</body>
</html>`
