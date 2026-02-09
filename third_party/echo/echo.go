package echo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type HandlerFunc func(Context) error
type MiddlewareFunc func(next HandlerFunc) HandlerFunc

type Context interface {
	Request() *http.Request
	Bind(any) error
	JSON(int, any) error
	Param(string) string
	QueryParam(string) string
}

type HTTPError struct {
	Code    int
	Message string
}

func (e *HTTPError) Error() string {
	return e.Message
}

type Echo struct {
	routes      []route
	middlewares []MiddlewareFunc
	routeMu     sync.RWMutex

	HideBanner bool
	HidePort   bool
}

type Group struct {
	echo   *Echo
	prefix string
}

type route struct {
	method   string
	pattern  string
	segments []string
	handler  HandlerFunc
}

type contextImpl struct {
	request *http.Request
	writer  http.ResponseWriter
	params  map[string]string
}

func New() *Echo {
	return &Echo{
		routes:      make([]route, 0, 16),
		middlewares: make([]MiddlewareFunc, 0, 4),
	}
}

func (e *Echo) Use(middlewares ...MiddlewareFunc) {
	e.middlewares = append(e.middlewares, middlewares...)
}

func (e *Echo) Group(prefix string) *Group {
	return &Group{
		echo:   e,
		prefix: normalizePath(prefix),
	}
}

func (e *Echo) GET(path string, h HandlerFunc) {
	e.add(http.MethodGet, path, h)
}

func (e *Echo) POST(path string, h HandlerFunc) {
	e.add(http.MethodPost, path, h)
}

func (e *Echo) DELETE(path string, h HandlerFunc) {
	e.add(http.MethodDelete, path, h)
}

func (g *Group) GET(path string, h HandlerFunc) {
	g.echo.add(http.MethodGet, joinPath(g.prefix, path), h)
}

func (g *Group) POST(path string, h HandlerFunc) {
	g.echo.add(http.MethodPost, joinPath(g.prefix, path), h)
}

func (g *Group) DELETE(path string, h HandlerFunc) {
	g.echo.add(http.MethodDelete, joinPath(g.prefix, path), h)
}

func (e *Echo) Start(addr string) error {
	return http.ListenAndServe(addr, e)
}

func (e *Echo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := normalizePath(r.URL.Path)

	e.routeMu.RLock()
	defer e.routeMu.RUnlock()

	for _, rt := range e.routes {
		if rt.method != r.Method {
			continue
		}

		params, ok := matchSegments(rt.segments, splitPath(path))
		if !ok {
			continue
		}

		ctx := &contextImpl{
			request: r,
			writer:  w,
			params:  params,
		}

		handler := rt.handler
		for idx := len(e.middlewares) - 1; idx >= 0; idx-- {
			handler = e.middlewares[idx](handler)
		}

		if err := handler(ctx); err != nil {
			writeHTTPError(w, err)
		}
		return
	}

	http.NotFound(w, r)
}

func (e *Echo) add(method, path string, h HandlerFunc) {
	e.routeMu.Lock()
	defer e.routeMu.Unlock()

	normalized := normalizePath(path)
	e.routes = append(e.routes, route{
		method:   method,
		pattern:  normalized,
		segments: splitPath(normalized),
		handler:  h,
	})
}

func (c *contextImpl) Request() *http.Request {
	return c.request
}

func (c *contextImpl) Bind(target any) error {
	if c.request.Body == nil {
		return errors.New("request body is empty")
	}

	decoder := json.NewDecoder(c.request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func (c *contextImpl) JSON(status int, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writer.Header().Set("Content-Type", "application/json")
	c.writer.WriteHeader(status)
	_, err = c.writer.Write(data)
	return err
}

func (c *contextImpl) Param(name string) string {
	return c.params[name]
}

func (c *contextImpl) QueryParam(name string) string {
	return c.request.URL.Query().Get(name)
}

func writeHTTPError(w http.ResponseWriter, err error) {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		w.WriteHeader(httpErr.Code)
		_, _ = w.Write([]byte(httpErr.Message))
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(err.Error()))
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func joinPath(prefix, sub string) string {
	if prefix == "/" {
		return normalizePath(sub)
	}
	return normalizePath(fmt.Sprintf("%s/%s", strings.TrimSuffix(prefix, "/"), strings.TrimPrefix(sub, "/")))
}

func splitPath(path string) []string {
	if path == "/" {
		return nil
	}
	clean := strings.Trim(path, "/")
	if clean == "" {
		return nil
	}
	return strings.Split(clean, "/")
}

func matchSegments(routeSegments, requestSegments []string) (map[string]string, bool) {
	if len(routeSegments) != len(requestSegments) {
		return nil, false
	}
	params := map[string]string{}
	for idx := range routeSegments {
		segment := routeSegments[idx]
		value := requestSegments[idx]
		if strings.HasPrefix(segment, ":") {
			key := strings.TrimPrefix(segment, ":")
			if key != "" {
				params[key] = value
			}
			continue
		}
		if segment != value {
			return nil, false
		}
	}
	return params, true
}
