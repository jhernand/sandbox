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

// This file contains the implementation of the authentication middleware used by the server.

package server

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

// Make sure that the handler implements the HTTP handler interface:
var _ http.Handler = &authHandler{}

// authHandler is the authentication handler used by the server. It checks that HTTP requests
// contain the authentication token in the Authorization header.
type authHandler struct {
	token string
	next  http.Handler
}

// ServeHTTP is the implementation of the HTTP handler interface.
func (h *authHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get the authentication header:
	authorization := r.Header.Get("Authorization")
	if authorization == "" {
		sendError(w, r, http.StatusBadRequest, "Authorization header is mandatory")
		return
	}

	// Extract the type and token:
	chunks := strings.Split(authorization, " ")
	count := len(chunks)
	if count != 2 {
		sendError(
			w, r,
			http.StatusBadRequest,
			"Expected exactly 2 parts in the authorization header but found %d",
			count,
		)
		return
	}
	typ := chunks[0]
	token := chunks[1]

	// Check that the type is bearer:
	if !strings.EqualFold(typ, "bearer") {
		sendError(
			w, r,
			http.StatusBadRequest,
			"Expected authorization type 'bearer' but found '%s'",
			typ,
		)
		return
	}

	// Check the value of the token:
	if token != h.token {
		log.WithFields(log.Fields{
			"method":  r.Method,
			"path":    r.URL.Path,
			"address": r.RemoteAddr,
			"token":   token,
		}).Info("Rejected request because token is incorrect")
		sendError(w, r, http.StatusUnauthorized, "Wrong token")
		return
	}

	// Everything is OK; call the next handler.
	h.next.ServeHTTP(w, r)
}

// authMiddleware receives a handler and wraps it with another that performs authentication using
// the given token.
func authMiddleware(token string) mux.MiddlewareFunc {
	return func(handler http.Handler) http.Handler {
		return &authHandler{
			token: token,
			next:  handler,
		}
	}
}
