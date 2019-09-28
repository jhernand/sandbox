/*
Copyright (c) 2019 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This file contains the implementation of the access log middleware used by the server.

package server

import (
	"net/http"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

// Make sure that the handler implements the HTTP handler interface:
var _ http.Handler = &accessLogHandler{}

// accessLogHandler is the authentication access log handler used by the server.
type accessLogHandler struct {
	next http.Handler
}

// ServeHTTP is the implementation of the HTTP handler interface.
func (h *accessLogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Print some details of the request:
	log.Infof("Received %s request for '%s' from '%s'", r.Method, r.URL.Path, r.RemoteAddr)

	// Call the next handler.
	h.next.ServeHTTP(w, r)
}

// accessLogMiddleware receives a handler and wraps it with another that writes the request to the
// access log.
func accessLogMiddleware() mux.MiddlewareFunc {
	return func(handler http.Handler) http.Handler {
		return &accessLogHandler{
			next: handler,
		}
	}
}
