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
	settingsservice "github.com/rigel-labs/rigel-build-engine/internal/service/settings"
)

type App struct {
	cfg      config.Config
	builder  *buildservice.Service
	advisor  *adviceservice.Service
	settings *settingsservice.Service
}

func New(cfg config.Config, builder *buildservice.Service, advisor *adviceservice.Service, settings *settingsservice.Service) *App {
	return &App{cfg: cfg, builder: builder, advisor: advisor, settings: settings}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/api/v1/catalog/prices", a.handleCatalogPrices)
	mux.HandleFunc("/api/v1/advice/catalog", a.handleGenerateCatalogAdvice)
	mux.HandleFunc("/api/v1/recommend/build", a.handleRecommendBuild)
	mux.HandleFunc("/admin/api/v1/settings/system", a.handleSystemSettings)
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
			"POST /api/v1/recommend/build",
			"GET /admin/api/v1/settings/system",
			"PUT /admin/api/v1/settings/system",
		},
	})
}

func (a *App) handleGenerateCatalogAdvice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireInternalServiceToken(w, r) {
		return
	}
	var req adviceservice.GenerateCatalogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := a.advisor.GenerateFromCatalog(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleRecommendBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireInternalServiceToken(w, r) {
		return
	}
	var req adviceservice.BuildRecommendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	catalog, err := a.builder.GeneratePriceCatalog(r.Context(), buildservice.CatalogRequest{
		UseCase:   req.UseCase,
		BuildMode: req.BuildMode,
		Limit:     500,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	response, err := a.advisor.GenerateBuildRecommendation(r.Context(), req, catalog)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	if a.settings == nil {
		writeError(w, http.StatusNotImplemented, "settings service not configured")
		return
	}
	if !a.requireBuildEngineAdminToken(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		view, err := a.settings.GetView(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	case http.MethodPut:
		var req settingsservice.UpdateSystemSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if err := a.settings.Update(r.Context(), req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		view, err := a.settings.GetView(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) requireBuildEngineAdminToken(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(a.cfg.AdminAPIToken) == "" {
		writeError(w, http.StatusServiceUnavailable, "build-engine admin token is not configured")
		return false
	}
	if strings.TrimSpace(r.Header.Get("X-Rigel-Admin-Token")) != strings.TrimSpace(a.cfg.AdminAPIToken) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (a *App) handleCatalogPrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireInternalServiceToken(w, r) {
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

func (a *App) requireInternalServiceToken(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimSpace(a.cfg.InternalServiceToken)
	if token == "" {
		writeError(w, http.StatusServiceUnavailable, "internal service token is not configured")
		return false
	}
	if strings.TrimSpace(r.Header.Get("X-Rigel-Service-Token")) != token {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
