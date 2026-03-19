package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/rigel-labs/rigel-build-engine/internal/config"
	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	adviceservice "github.com/rigel-labs/rigel-build-engine/internal/service/advice"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

type App struct {
	cfg         config.Config
	builder     *buildservice.Service
	advisor     *adviceservice.Service
	adviceSlots chan struct{}
}

func New(cfg config.Config, builder *buildservice.Service, advisor *adviceservice.Service) *App {
	slots := make(chan struct{}, max(1, cfg.AdviceMaxConcurrency))
	return &App{cfg: cfg, builder: builder, advisor: advisor, adviceSlots: slots}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/api/v1/catalog/prices", a.handleCatalogPrices)
	mux.HandleFunc("/api/v1/advice/catalog", a.handleGenerateCatalogAdvice)
	mux.HandleFunc("/", a.handleIndex)
	return mux
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": a.cfg.ServiceName, "mode": a.cfg.BuildEngineMode})
}

func (a *App) handleIndex(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": a.cfg.ServiceName,
		"mode":    a.cfg.BuildEngineMode,
		"routes": []string{
			"GET /healthz",
			"GET /api/v1/catalog/prices",
			"POST /api/v1/advice/catalog",
		},
	})
}

func (a *App) handleGenerateCatalogAdvice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized service request")
		return
	}
	var req adviceservice.GenerateCatalogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	select {
	case a.adviceSlots <- struct{}{}:
		defer func() { <-a.adviceSlots }()
	default:
		writeError(w, http.StatusTooManyRequests, "advice concurrency limit reached")
		return
	}
	response, err := a.advisor.GenerateFromCatalog(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleCatalogPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized service request")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	useCase := model.UseCase(strings.TrimSpace(r.URL.Query().Get("use_case")))
	buildMode := model.BuildMode(strings.TrimSpace(r.URL.Query().Get("build_mode")))
	response, err := a.builder.GeneratePriceCatalog(r.Context(), buildservice.CatalogRequest{
		UseCase:   useCase,
		BuildMode: buildMode,
		Limit:     limit,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *App) authorized(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("X-Rigel-Service-Token")) == a.cfg.InternalServiceToken
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
