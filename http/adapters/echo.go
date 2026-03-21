package adapters

import (
	"net/http"

	"github.com/labstack/echo/v4"
	rlhttp "github.com/ratrektlabs/rl-agent/http"
)

func WrapEchoHandler(h *rlhttp.AgentHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

func SetupEchoRouter(e *echo.Echo, h *rlhttp.AgentHandler) {
	api := e.Group("/api")
	{
		api.POST("/run", WrapEchoHandler(h))
		api.POST("/stream", WrapEchoHandler(h))
		api.GET("/tools", WrapEchoHandler(h))
		api.POST("/tools", WrapEchoHandler(h))
		api.GET("/skills", WrapEchoHandler(h))
		api.POST("/skills", WrapEchoHandler(h))
		api.GET("/health", WrapEchoHandler(h))
	}
}

func NewEchoRouter(h *rlhttp.AgentHandler, middleware ...echo.MiddlewareFunc) *echo.Echo {
	e := echo.New()
	for _, m := range middleware {
		e.Use(m)
	}
	SetupEchoRouter(e, h)
	return e
}

type EchoAdapter struct {
	handler *rlhttp.AgentHandler
	echo    *echo.Echo
}

func NewEchoAdapter(h *rlhttp.AgentHandler) *EchoAdapter {
	return &EchoAdapter{
		handler: h,
		echo:    echo.New(),
	}
}

func (a *EchoAdapter) Use(middleware ...echo.MiddlewareFunc) *EchoAdapter {
	a.echo.Use(middleware...)
	return a
}

func (a *EchoAdapter) SetupRoutes() *EchoAdapter {
	SetupEchoRouter(a.echo, a.handler)
	return a
}

func (a *EchoAdapter) Echo() *echo.Echo {
	return a.echo
}

func (a *EchoAdapter) Start(addr string) error {
	return a.echo.Start(addr)
}

func EchoMiddleware(m rlhttp.Middleware) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.Request = r
			})
			wrapped := m(h)
			wrapped.ServeHTTP(c.Response(), c.Request())
			return next(c)
		}
	}
}
