package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/alperreha/mergen-fire/internal/manager"
	"github.com/alperreha/mergen-fire/internal/model"
)

type Handler struct {
	service *manager.Service
}

func Register(e *echo.Echo, service *manager.Service) {
	handler := &Handler{service: service}

	v1 := e.Group("/v1")
	v1.POST("/vms", handler.createVM)
	v1.POST("/vms/:id/start", handler.startVM)
	v1.POST("/vms/:id/stop", handler.stopVM)
	v1.DELETE("/vms/:id", handler.deleteVM)
	v1.GET("/vms/:id", handler.getVM)
	v1.GET("/vms", handler.listVMs)
}

func (h *Handler) createVM(c echo.Context) error {
	var req model.CreateVMRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse("bad_request", err))
	}

	id, err := h.service.CreateVM(c.Request().Context(), req)
	if err != nil {
		return writeServiceError(c, err)
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"id":     id,
		"status": "created",
	})
}

func (h *Handler) startVM(c echo.Context) error {
	id := c.Param("id")
	if err := h.service.StartVM(c.Request().Context(), id); err != nil {
		return writeServiceError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"id":     id,
		"status": "started",
	})
}

func (h *Handler) stopVM(c echo.Context) error {
	id := c.Param("id")
	if err := h.service.StopVM(c.Request().Context(), id); err != nil {
		return writeServiceError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"id":     id,
		"status": "stopped",
	})
}

func (h *Handler) deleteVM(c echo.Context) error {
	id := c.Param("id")
	retainData, err := parseBool(c.QueryParam("retainData"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse("bad_request", err))
	}
	if err := h.service.DeleteVM(c.Request().Context(), id, retainData); err != nil {
		return writeServiceError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"id":     id,
		"status": "deleted",
	})
}

func (h *Handler) getVM(c echo.Context) error {
	vm, err := h.service.GetVM(c.Request().Context(), c.Param("id"))
	if err != nil {
		return writeServiceError(c, err)
	}
	return c.JSON(http.StatusOK, vm)
}

func (h *Handler) listVMs(c echo.Context) error {
	vms, err := h.service.ListVMs(c.Request().Context())
	if err != nil {
		return writeServiceError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"items": vms})
}

func writeServiceError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, manager.ErrInvalidRequest):
		return c.JSON(http.StatusBadRequest, errorResponse("bad_request", err))
	case errors.Is(err, manager.ErrNotFound):
		return c.JSON(http.StatusNotFound, errorResponse("not_found", err))
	case errors.Is(err, manager.ErrConflict):
		return c.JSON(http.StatusConflict, errorResponse("conflict", err))
	case errors.Is(err, manager.ErrUnavailable):
		return c.JSON(http.StatusServiceUnavailable, errorResponse("dependency_unavailable", err))
	default:
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
