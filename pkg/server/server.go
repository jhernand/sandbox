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

// This file contains the implementation of the test runner that and executes test binaries.

package server

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

// ServerBuilder contains the information and logic needed to create a test runner server. Don't
// create instances of this type directly; use the NewServer function instead.
type ServerBuilder struct {
	listen string
	token  string
	work   string
}

// Server is the test runner server.
type Server struct {
	listen string
	token  string
	work   string
	ws     *http.Server
}

// NewServer creates a new object that knows how to build servers.
func NewServer() *ServerBuilder {
	return &ServerBuilder{}
}

// Listen sets the address and port number where the server will be listening. If not specified
// it will listen in all the addresses available and in port 8000.
func (b *ServerBuilder) Listen(value string) *ServerBuilder {
	b.listen = value
	return b
}

// Token sets the authentication token that will be required in all the HTTP requests.
func (b *ServerBuilder) Token(value string) *ServerBuilder {
	b.token = value
	return b
}

// Work sets the directory where the server will copy and execute the test binaries.
func (b *ServerBuilder) Work(value string) *ServerBuilder {
	b.work = value
	return b
}

// Build uses the information stored in the builder to create a new server. Note that the returned
// server isn't started yet. To start it call the Start method.
func (b *ServerBuilder) Build() (srvr *Server, err error) {
	// Check parameters:
	if b.token == "" {
		err = fmt.Errorf("work directory is mandatory")
		return
	}

	// Check that the working directory exists:
	work := b.work
	if work == "" {
		work = os.TempDir()
	}
	_, err = os.Stat(work)
	if os.IsNotExist(err) {
		err = fmt.Errorf("working directory '%s' doesn't exist", work)
		return
	}
	if err != nil {
		err = fmt.Errorf("can't check if working directory '%s' exists: %v", work, err)
		return
	}

	// Create and populate the object:
	srvr = &Server{
		listen: b.listen,
		token:  b.token,
		work:   work,
	}

	return
}

// Start starts the server.
func (s *Server) Start() error {
	// Create the main router:
	router := mux.NewRouter()
	router.NotFoundHandler = &notFoundHandler{}
	router.Use(accessLogMiddleware())
	router.Use(authMiddleware(s.token))

	// Create the test handler:
	handler := &postTestHandler{
		work: s.work,
	}

	// Register the API handlers:
	// apiRouter := mainRouter.Path(apiPrefix).Subrouter()
	// versionRouter := apiRouter.Path("/"+apiVersion).Subrouter()
	router.Handle("/api/v1/tests", handler).Methods(http.MethodPost)

	// Create the HTTP server:
	s.ws = &http.Server{
		Addr:    s.listen,
		Handler: router,
	}
	go func() {
		err := s.ws.ListenAndServe()
		if err != nil {
			log.WithError(err).Info("Web server finished with error")
		}
	}()

	return nil
}

// Stop stops the server.
func (s *Server) Stop() error {
	// Try to stop the web server:
	if s.ws != nil {
		err := s.ws.Shutdown(context.Background())
		if err != nil {
			return err
		}
	}

	return nil
}

// Destroy releases all the resources used by the server.
func (c *Server) Destroy() error {
	return nil
}
