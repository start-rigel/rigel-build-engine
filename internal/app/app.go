package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rigel-labs/rigel-build-engine/internal/config"
	"github.com/rigel-labs/rigel-build-engine/internal/domain/model"
	adviceservice "github.com/rigel-labs/rigel-build-engine/internal/service/advice"
	buildservice "github.com/rigel-labs/rigel-build-engine/internal/service/build"
)

type App struct {
	cfg     config.Config
	builder *buildservice.Service
	advisor *adviceservice.Service
}

func New(cfg config.Config, builder *buildservice.Service, advisor *adviceservice.Service) *App {
	return &App{cfg: cfg, builder: builder, advisor: advisor}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/api/v1/catalog/prices", a.handleCatalogPrices)
	mux.HandleFunc("/api/v1/advice/generate", a.handleGenerateAdvice)
	mux.HandleFunc("/api/v1/advice/catalog", a.handleGenerateCatalogAdvice)
	mux.HandleFunc("/api/v1/builds/generate", a.handleGenerate)
	mux.HandleFunc("/api/v1/builds/", a.handleGetBuild)
	mux.HandleFunc("/api/v1/parts/search", a.handleSearchParts)
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
			"POST /api/v1/advice/generate",
			"POST /api/v1/advice/catalog",
			"POST /api/v1/builds/generate",
			"GET /api/v1/builds/{id}",
			"GET /api/v1/parts/search",
		},
	})
}

func (a *App) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req buildservice.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := a.builder.Generate(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleGenerateAdvice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req adviceservice.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	response, err := a.advisor.Generate(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) handleGenerateCatalogAdvice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req adviceservice.GenerateCatalogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
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

func (a *App) handleSearchParts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	parts, err := a.builder.SearchParts(r.Context(), strings.TrimSpace(r.URL.Query().Get("keyword")), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(parts), "items": parts})
}

func (a *App) handleGetBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	requestID := strings.TrimPrefix(r.URL.Path, "/api/v1/builds/")
	if requestID == "" || requestID == "/" {
		writeError(w, http.StatusBadRequest, "build request id is required")
		return
	}
	response, err := a.builder.GetBuild(r.Context(), requestID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
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
