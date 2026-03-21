package adapters

import (
	"net/http"

	"github.com/gin-gonic/gin"
	rlhttp "github.com/ratrektlabs/rl-agent/http"
)

func WrapGinHandler(h *rlhttp.AgentHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

func SetupGinRouter(r *gin.Engine, h *rlhttp.AgentHandler) {
	api := r.Group("/api")
	{
		api.POST("/run", WrapGinHandler(h))
		api.POST("/stream", WrapGinHandler(h))
		api.GET("/tools", WrapGinHandler(h))
		api.POST("/tools", WrapGinHandler(h))
		api.GET("/skills", WrapGinHandler(h))
		api.POST("/skills", WrapGinHandler(h))
		api.GET("/health", WrapGinHandler(h))
	}
}

func NewGinRouter(h *rlhttp.AgentHandler, middleware ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	for _, m := range middleware {
		r.Use(m)
	}
	SetupGinRouter(r, h)
	return r
}

type GinAdapter struct {
	handler *rlhttp.AgentHandler
	engine  *gin.Engine
}

func NewGinAdapter(h *rlhttp.AgentHandler) *GinAdapter {
	return &GinAdapter{
		handler: h,
		engine:  gin.New(),
	}
}

func (a *GinAdapter) Use(middleware ...gin.HandlerFunc) *GinAdapter {
	a.engine.Use(middleware...)
	return a
}

func (a *GinAdapter) SetupRoutes() *GinAdapter {
	SetupGinRouter(a.engine, a.handler)
	return a
}

func (a *GinAdapter) Engine() *gin.Engine {
	return a.engine
}

func (a *GinAdapter) Run(addr string) error {
	return a.engine.Run(addr)
}

func GinMiddleware(m rlhttp.Middleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.Request = r
			c.Writer = gin.ResponseWriter(w)
			c.Next()
		})
		m(h).ServeHTTP(c.Writer, c.Request)
	}
}
