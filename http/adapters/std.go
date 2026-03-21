package adapters

import (
	"net/http"

	rlhttp "github.com/ratrektlabs/rl-agent/http"
)

func WrapStdHandler(h *rlhttp.AgentHandler) http.HandlerFunc {
	return h.ServeHTTP
}

func NewStdMux(h *rlhttp.AgentHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/run", h.ServeHTTP)
	mux.HandleFunc("/stream", h.ServeHTTP)
	mux.HandleFunc("/tools", h.ServeHTTP)
	mux.HandleFunc("/skills", h.ServeHTTP)
	mux.HandleFunc("/health", h.ServeHTTP)
	return mux
}

func NewStdServer(addr string, h *rlhttp.AgentHandler, middleware ...rlhttp.Middleware) *http.Server {
	handler := rlhttp.Chain(h, middleware...)
	return &http.Server{
		Addr:    addr,
		Handler: handler,
	}
}

type StdRouter struct {
	mux        *http.ServeMux
	middleware []rlhttp.Middleware
}

func NewStdRouter() *StdRouter {
	return &StdRouter{
		mux: http.NewServeMux(),
	}
}

func (r *StdRouter) Use(m rlhttp.Middleware) {
	r.middleware = append(r.middleware, m)
}

func (r *StdRouter) Handle(pattern string, handler http.Handler) {
	h := rlhttp.Chain(handler, r.middleware...)
	r.mux.Handle(pattern, h)
}

func (r *StdRouter) HandleFunc(pattern string, handler http.HandlerFunc) {
	r.Handle(pattern, handler)
}

func (r *StdRouter) MountAgent(h *rlhttp.AgentHandler) {
	r.HandleFunc("/run", h.ServeHTTP)
	r.HandleFunc("/stream", h.ServeHTTP)
	r.HandleFunc("/tools", h.ServeHTTP)
	r.HandleFunc("/skills", h.ServeHTTP)
	r.HandleFunc("/health", h.ServeHTTP)
}

func (r *StdRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *StdRouter) Mux() *http.ServeMux {
	return r.mux
}
