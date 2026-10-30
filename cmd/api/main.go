package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"tessera/internal/auth"
	"tessera/internal/config"
)

type apiResponse struct {
	OK    bool   `json:"ok"`
	Msg   string `json:"message,omitempty"`
	Quote string `json:"quote_id,omitempty"`
	Trip  string `json:"trip_id,omitempty"`
}

func main() {
	cfg := config.Load()
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Msg: "ok"})
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"uptime_seconds": 0,
			"status":         "ok",
		})
	})
	mux.HandleFunc("/v1/admin/surge/cells", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResponse{OK: false, Msg: "method not allowed"})
			return
		}
		if err := auth.RequireRole(auth.RoleAdmin, strings.TrimSpace(r.Header.Get("Authorization"))); err != nil {
			writeJSON(w, http.StatusUnauthorized, apiResponse{OK: false, Msg: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"cells": []map[string]any{{
				"cell":          "d0001",
				"multiplier_bps": 10000,
				"demand":        0,
				"supply":        0,
			}},
		})
	})
	mux.HandleFunc("/v1/admin/pricing/rule-sets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResponse{OK: false, Msg: "method not allowed"})
			return
		}
		if err := auth.RequireRole(auth.RoleAdmin, strings.TrimSpace(r.Header.Get("Authorization"))); err != nil {
			writeJSON(w, http.StatusUnauthorized, apiResponse{OK: false, Msg: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rule_sets": []string{"default-v1"}})
	})
	mux.HandleFunc("/v1/admin/reconciliation", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, apiResponse{OK: false, Msg: "method not allowed"})
			return
		}
		if err := auth.RequireRole(auth.RoleAdmin, strings.TrimSpace(r.Header.Get("Authorization"))); err != nil {
			writeJSON(w, http.StatusUnauthorized, apiResponse{OK: false, Msg: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "findings": []string{}})
	})
	mux.HandleFunc("/v1/rides/quote", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, apiResponse{OK: false, Msg: "method not allowed"})
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Msg: "quote created", Quote: "quote-demo-1"})
	})
	mux.HandleFunc("/v1/rides", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, apiResponse{OK: false, Msg: "method not allowed"})
			return
		}
		token := strings.TrimSpace(r.Header.Get("Authorization"))
		if err := auth.RequireRole(auth.RoleRider, token); err != nil {
			writeJSON(w, http.StatusUnauthorized, apiResponse{OK: false, Msg: err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, apiResponse{OK: true, Msg: "ride created", Trip: "trip-demo-1"})
	})
	mux.HandleFunc("/v1/drivers/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/drivers/")
		if strings.HasSuffix(path, "/online") {
			if err := auth.RequireRole(auth.RoleDriver, strings.TrimSpace(r.Header.Get("Authorization"))); err != nil {
				writeJSON(w, http.StatusUnauthorized, apiResponse{OK: false, Msg: err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, apiResponse{OK: true, Msg: "driver online"})
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Msg: fmt.Sprintf("driver action for %s", path)})
	})
	mux.HandleFunc("/v1/offers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, apiResponse{OK: false, Msg: "method not allowed"})
			return
		}
		if err := auth.RequireRole(auth.RoleDriver, strings.TrimSpace(r.Header.Get("Authorization"))); err != nil {
			writeJSON(w, http.StatusUnauthorized, apiResponse{OK: false, Msg: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Msg: "offer handled"})
	})
	mux.HandleFunc("/v1/trips/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, apiResponse{OK: false, Msg: "method not allowed"})
			return
		}
		if err := auth.RequireRole(auth.RoleDriver, strings.TrimSpace(r.Header.Get("Authorization"))); err != nil {
			writeJSON(w, http.StatusUnauthorized, apiResponse{OK: false, Msg: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, apiResponse{OK: true, Msg: "trip updated"})
	})

	log.Printf("Tessera API starting on %s:%s", cfg.Host, cfg.Port)
	if err := http.ListenAndServe(cfg.Host+":"+cfg.Port, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("encode response: %v", err)
	}
}
