package router

import (
	"net/http"
	"strings"
)

// The service can be mounted a second time under RELAY_BASE_PATH so that an
// external gateway forwards prefixed traffic here. That entrypoint is public,
// so it only exposes model invocation endpoints plus the CSL hourly log
// queries; the dashboard API, admin API and web assets stay reachable on the
// root path only.
var (
	relayBasePathExactPaths = map[string]bool{
		"/api/log/hourly":      true,
		"/api/log/self/hourly": true,
	}

	relayBasePathPrefixes = []string{
		"/v1/",     // OpenAI compatible relay, video relay, model list, billing usage
		"/v1beta/", // Gemini relay
		"/mj/",     // Midjourney relay
		"/suno/",   // Suno relay
		"/kling/",  // Kling relay
		"/jimeng",  // Jimeng relay
	}
)

const relayBasePathNotFoundBody = `{"error":{"message":"This endpoint is not exposed on the relay base path","type":"invalid_request_error","code":"endpoint_not_exposed"}}`

// RelayBasePathAllows reports whether path, already stripped of the configured
// base path, is one of the endpoints exposed under RELAY_BASE_PATH.
func RelayBasePathAllows(path string) bool {
	if relayBasePathExactPaths[path] {
		return true
	}
	for _, prefix := range relayBasePathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	// Midjourney is also registered with a leading mode segment: /:mode/mj/...
	if idx := strings.Index(path, "/mj/"); idx > 0 && strings.Count(path[:idx], "/") == 1 {
		return true
	}
	return false
}

// WrapRelayBasePath serves handler on the root path and, when basePath is not
// empty, additionally under basePath with only the endpoints allowed by
// RelayBasePathAllows. Anything else under basePath gets a JSON 404 instead of
// being forwarded.
func WrapRelayBasePath(basePath string, handler http.Handler) http.Handler {
	if basePath == "" {
		return handler
	}
	mux := http.NewServeMux()
	mux.Handle(basePath+"/", http.StripPrefix(basePath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawPath = ""
		if !RelayBasePathAllows(r.URL.Path) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(relayBasePathNotFoundBody))
			return
		}
		handler.ServeHTTP(w, r)
	})))
	mux.HandleFunc(basePath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	})
	// Keep root accessible so the built-in healthcheck (/api/status) still works.
	mux.Handle("/", handler)
	return mux
}
