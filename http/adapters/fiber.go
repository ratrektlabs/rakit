package adapters

import (
	rlhttp "github.com/ratrektlabs/rl-agent/http"
)

func AdaptFiberHandler(h http.HandlerFunc) fiber.Handler {
	return func(c *fiber.Ctx) error {
		h(c.Writer, c.Request)
		return nil
}

func SetupFiberRouter(app *fiber.App, h *rlhttp.AgentHandler) {
	api := app.Group("/api")
	api.Post("/run", AdaptFiberHandler(h))
	api.Post("/stream", AdaptFiberHandler(h))
	api.Get("/tools", AdaptFiberHandler(h))
	api.Post("/tools", AdaptFiberHandler(h))
	api.Get("/skills", AdaptFiberHandler(h))
	api.Post("/skills", AdaptFiberHandler(h))
	api.Get("/health", AdaptFiberHandler(h))
}

func NewFiberRouter(h *rlhttp.AgentHandler, middleware ...fiber.Handler) *fiber.App {
	app := fiber.New()
	for _, m := range middleware {
	 app.Use(m)
	}
    SetupFiberRouter(app, h)
    return app
}

type FiberAdapter struct {
	handler *rlhttp.AgentHandler
    app     *fiber.App
}

func NewFiberAdapter(h *rlhttp.AgentHandler) *FiberAdapter {
    return &FiberAdapter{
        handler: h,
        app:     fiber.New(),
    }
}

func (a *FiberAdapter) Use(middleware ...fiber.Handler) *FiberAdapter {
    a.app.Use(middleware...)
    return a
}

func (a *FiberAdapter) SetupRoutes() *FiberAdapter {
    SetupFiberRouter(a.app, a.handler)
    return a
}

func (a *FiberAdapter) App() *fiber.App {
    return a.app
}

func (a *FiberAdapter) Listen(addr string) error {
    return a.app.Listen(addr)
}
