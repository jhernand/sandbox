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

// This file contains the code that deploys the server that runs inside the temporary project, as
// well as the Server type that simplifies the interaction with that server using its REST API.

package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/jhernand/sandbox/pkg/api"
)

// Server simplifies the interaction with the server.
type Server struct {
	// Token and address of the server:
	token   string
	address string

	// HTTP client:
	client *http.Client
}

// Send sends the test to the server, waits for it to be executed and returns the results.
func (s *Server) Send(request *api.Test) (response *api.Test, err error) {
	// Calculate the request address:
	httpAddress := fmt.Sprintf("%s%s/%s/tests", s.address, api.Prefix, api.Version)
	log.Debugf("Sending POST request to '%s'", httpAddress)

	// Serialize the request body:
	httpBody := new(bytes.Buffer)
	err = json.NewEncoder(httpBody).Encode(request)
	if err != nil {
		return
	}

	// Prepare the authorization header:
	httpAuthorization := fmt.Sprintf("Bearer %s", s.token)

	// Send the HTTP request:
	httpRequest, err := http.NewRequest(http.MethodPost, httpAddress, httpBody)
	if err != nil {
		return
	}
	httpRequest.Header.Set("Authorization", httpAuthorization)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpResponse, err := s.client.Do(httpRequest)
	if err != nil {
		return
	}
	httpClose := func() {
		err := httpResponse.Body.Close()
		if err != nil {
			log.Errorf("Can't close response body: %v", err)
		}
	}
	defer httpClose()
	if httpResponse.StatusCode != http.StatusOK {
		err = fmt.Errorf("send failed with status code %d", httpResponse.StatusCode)
		return
	}

	// Deserialize the response body:
	response = &api.Test{}
	err = json.NewDecoder(httpResponse.Body).Decode(response)
	if err != nil {
		return
	}

	return
}

// Address returns the address of the server.
func (s *Server) Address() string {
	return s.address
}

// Server returns the object that is used to interact with the server.
func (r *Runner) Server() *Server {
	return r.server
}
