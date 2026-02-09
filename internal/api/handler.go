package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/alperreha/mergen-fire/internal/manager"
	"github.com/alperreha/mergen-fire/internal/model"
)

type Handler struct {
	service *manager.Service
	logger  *slog.Logger
}

func Register(e *echo.Echo, service *manager.Service, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	handler := &Handler{service: service, logger: logger}

	v1 := e.Group("/v1")
	v1.POST("/vms", handler.createVM)
	v1.POST("/vms/:id/start", handler.startVM)
	v1.POST("/vms/:id/stop", handler.stopVM)
	v1.DELETE("/vms/:id", handler.deleteVM)
	v1.GET("/vms/:id", handler.getVM)
	v1.GET("/vms", handler.listVMs)
}

func (h *Handler) createVM(c echo.Context) error {
	h.logger.Debug("http create vm", "method", c.Request().Method, "path", c.Request().URL.Path)
	var req model.CreateVMRequest
	if err := c.Bind(&req); err != nil {
		h.logger.Debug("http create vm bind failed", "error", err)
		return c.JSON(http.StatusBadRequest, errorResponse("bad_request", err))
	}
	h.logger.Debug("http create vm payload parsed", "rootfs", req.RootFS, "kernel", req.Kernel, "vcpu", req.VCPU, "memMiB", req.MemMiB, "ports", len(req.Ports), "autoStart", req.AutoStart)

	id, err := h.service.CreateVM(c.Request().Context(), req)
	if err != nil {
		return h.writeServiceError(c, err)
	}
	h.logger.Info("http create vm success", "vmID", id)

	return c.JSON(http.StatusCreated, map[string]any{
		"id":     id,
		"status": "created",
	})
}

func (h *Handler) startVM(c echo.Context) error {
	id := c.Param("id")
	h.logger.Debug("http start vm", "vmID", id, "method", c.Request().Method, "path", c.Request().URL.Path)
	if err := h.service.StartVM(c.Request().Context(), id); err != nil {
		return h.writeServiceError(c, err)
	}
	h.logger.Info("http start vm success", "vmID", id)
	return c.JSON(http.StatusOK, map[string]any{
		"id":     id,
		"status": "started",
	})
}

func (h *Handler) stopVM(c echo.Context) error {
	id := c.Param("id")
	h.logger.Debug("http stop vm", "vmID", id, "method", c.Request().Method, "path", c.Request().URL.Path)
	if err := h.service.StopVM(c.Request().Context(), id); err != nil {
		return h.writeServiceError(c, err)
	}
	h.logger.Info("http stop vm success", "vmID", id)
	return c.JSON(http.StatusOK, map[string]any{
		"id":     id,
		"status": "stopped",
	})
}

func (h *Handler) deleteVM(c echo.Context) error {
	id := c.Param("id")
	h.logger.Debug("http delete vm", "vmID", id, "method", c.Request().Method, "path", c.Request().URL.Path, "retainDataRaw", c.QueryParam("retainData"))
	retainData, err := parseBool(c.QueryParam("retainData"))
	if err != nil {
		h.logger.Debug("http delete vm query parse failed", "vmID", id, "error", err)
		return c.JSON(http.StatusBadRequest, errorResponse("bad_request", err))
	}
	if err := h.service.DeleteVM(c.Request().Context(), id, retainData); err != nil {
		return h.writeServiceError(c, err)
	}
	h.logger.Info("http delete vm success", "vmID", id, "retainData", retainData)
	return c.JSON(http.StatusOK, map[string]any{
		"id":     id,
		"status": "deleted",
	})
}

func (h *Handler) getVM(c echo.Context) error {
	id := c.Param("id")
	h.logger.Debug("http get vm", "vmID", id, "method", c.Request().Method, "path", c.Request().URL.Path)
	vm, err := h.service.GetVM(c.Request().Context(), id)
	if err != nil {
		return h.writeServiceError(c, err)
	}
	h.logger.Debug("http get vm success", "vmID", id)
	return c.JSON(http.StatusOK, vm)
}

func (h *Handler) listVMs(c echo.Context) error {
	h.logger.Debug("http list vms", "method", c.Request().Method, "path", c.Request().URL.Path)
	vms, err := h.service.ListVMs(c.Request().Context())
	if err != nil {
		return h.writeServiceError(c, err)
	}
	h.logger.Debug("http list vms success", "count", len(vms))
	return c.JSON(http.StatusOK, map[string]any{"items": vms})
}

func (h *Handler) writeServiceError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, manager.ErrInvalidRequest):
		h.logger.Warn("http request failed", "status", http.StatusBadRequest, "error", err)
		return c.JSON(http.StatusBadRequest, errorResponse("bad_request", err))
	case errors.Is(err, manager.ErrNotFound):
		h.logger.Warn("http request failed", "status", http.StatusNotFound, "error", err)
		return c.JSON(http.StatusNotFound, errorResponse("not_found", err))
	case errors.Is(err, manager.ErrConflict):
		h.logger.Warn("http request failed", "status", http.StatusConflict, "error", err)
		return c.JSON(http.StatusConflict, errorResponse("conflict", err))
	case errors.Is(err, manager.ErrUnavailable):
		h.logger.Warn("http request failed", "status", http.StatusServiceUnavailable, "error", err)
		return c.JSON(http.StatusServiceUnavailable, errorResponse("dependency_unavailable", err))
	default:
		h.logger.Error("http request failed", "status", http.StatusInternalServerError, "error", err)
		return c.JSON(http.StatusInternalServerError, errorResponse("internal_error", err))
	}
}

func errorResponse(code string, err error) map[string]any {
	return map[string]any{
		"error":   code,
		"message": err.Error(),
	}
}

func parseBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	return strconv.ParseBool(value)
}
