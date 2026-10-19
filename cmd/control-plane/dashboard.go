package main

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed dashboard.html
var dashboardPage string

func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := strings.NewReplacer(
		`id="best-latency">18ms`, `id="best-latency">--`,
		`<div class="stat-note">Fiber primary</div>`, `<div class="stat-note">Awaiting probe</div>`,
		`<div class="stat-value">1.8 GB</div><div class="stat-note">Since 08:00 today</div>`, `<div class="stat-value">--</div><div class="stat-note">Awaiting telemetry</div>`,
		`<div class="stat-value">4.8/5</div><div class="stat-note">Excellent experience</div>`, `<div class="stat-value">--</div><div class="stat-note">Awaiting application SLO data</div>`,
	).Replace(dashboardPage)
	_, _ = w.Write([]byte(page))
}
