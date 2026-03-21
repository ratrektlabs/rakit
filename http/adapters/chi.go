package adapters

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	rlhttp "github.com/ratrektlabs/rl-agent/http"
)

func WrapChiHandler(h *rlhttp.AgentHandler) http.HandlerFunc {
	return h.ServeHTTP
}

func SetupChiRouter(r chi.Router, h *rlhttp.AgentHandler) {
	r.Route("/api", func(r chi.Router) {
		r.Post("/run", h.ServeHTTP)
		r.Post("/stream", h.ServeHTTP)
		r.Get("/tools", h.ServeHTTP)
		r.Post("/tools", h.ServeHTTP)
		r.Get("/skills", h.ServeHTTP)
		r.Post("/skills", h.ServeHTTP)
		r.Get("/health", h.ServeHTTP)
	})
}

func NewChiRouter(h *rlhttp.AgentHandler, middlewares ...func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()
	for _, m := range middlewares {
		r.Use(m)
	}
	SetupChiRouter(r, h)
	return r
}

type ChiAdapter struct {
	handler *rlhttp.AgentHandler
	router  chi.Router
}

func NewChiAdapter(h *rlhttp.AgentHandler) *ChiAdapter {
	return &ChiAdapter{
		handler: h,
		router:  chi.NewRouter(),
	}
}

func (a *ChiAdapter) Use(middlewares ...func(http.Handler) http.Handler) *ChiAdapter {
	a.router.Use(middlewares...)
	return a
}

func (a *ChiAdapter) SetupRoutes() *ChiAdapter {
	SetupChiRouter(a.router, a.handler)
	return a
}

func (a *ChiAdapter) Router() chi.Router {
	return a.router
}

func (a *ChiAdapter) Mount(pattern string, h http.Handler) *ChiAdapter {
	a.router.Mount(pattern, h)
	return a
}

func (a *ChiAdapter) Route(pattern string, fn func(chi.Router)) *ChiAdapter {
	a.router.Route(pattern, fn)
	return a
}

func ChiMiddleware(m rlhttp.Middleware) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return m(next)
	}
}

func NewChiServer(addr string, h *rlhttp.AgentHandler) *http.Server {
	r := NewChiRouter(h, middleware.Logger, middleware.Recoverer)
	return &http.Server{
		Addr:    addr,
		Handler: r,
	}
}
