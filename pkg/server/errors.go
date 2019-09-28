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

// This file contains the methods used by the server to send error responses.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/jhernand/sandbox/pkg/api"
)

// panicBody is the error body that will be sent when something unexpected happens while trying to
// send another error response. For example, if sending an error response fails because the error
// description can't be converted to JSON.
var panicBody []byte

func init() {
	var err error

	// Create the panic error body:
	panicError := &api.Error{
		Reason: "An unexpected error happened, please check the log for details",
	}

	// Convert it to JSON:
	panicBody, err = json.Marshal(panicError)
	if err != nil {
		log.Errorf("Can't create the panic error body: %v", err)
	}
}

// sendError sends an error response to the client.
func sendError(w http.ResponseWriter, r *http.Request, status int, format string,
	a ...interface{}) {
	// Set the content type:
	w.Header().Set("Content-Type", "application/json")

	// Marshal the body:
	reason := fmt.Sprintf(format, a...)
	body := &api.Error{
		Reason: reason,
	}
	data, err := json.Marshal(body)
	if err != nil {
		sendPanic(w, r)
		return
	}

	// Send the response:
	w.WriteHeader(status)
	_, err = w.Write(data)
	if err != nil {
		log.Errorf("Can't send response body for request '%s'", r.URL.Path)
		return
	}
}

// SendPanic sends a panic error response to the client, but it doesn't end the process.
func sendPanic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, err := w.Write(panicBody)
	if err != nil {
		log.Errorf("Can't send panic response for request '%s': %s", r.URL.Path, err)
	}
}
