package middleware

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

func Recover() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					err = &echo.HTTPError{
						Code:    http.StatusInternalServerError,
						Message: fmt.Sprintf("panic recovered: %v", recovered),
					}
				}
			}()
			return next(c)
		}
	}
}

func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return next(c)
		}
	}
}
