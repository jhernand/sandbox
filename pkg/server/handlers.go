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

// This file contains the implementation of the HTTP handlers.

package server

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/jhernand/sandbox/pkg/api"
)

// Make sure that the handler implements the HTTP handler interface:
var _ http.Handler = &notFoundHandler{}
var _ http.Handler = &postTestHandler{}

// notFoundHandler is an HTTP handler that returns a not found error response for all requests.
type notFoundHandler struct {
	// Empty on purpose.
}

// ServeHTTP is the implementation of the HTTP handler interface.
func (h *notFoundHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sendError(
		w, r,
		http.StatusNotFound,
		"Can't find resource for path '%s'",
		r.URL.Path,
	)
}

// postTestHandler is the handler that receives a POST containing a task description, runs it and
// returns the results.
type postTestHandler struct {
	work string
}

// ServeHTTP is the implementation of the HTTP handler interface.
func (h *postTestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Unmarshal the request body:
	requestBody := &api.Test{}
	requestDecoder := json.NewDecoder(r.Body)
	err := requestDecoder.Decode(requestBody)
	if err != nil {
		log.WithError(err).Info("Can't unmarshal request body")
		sendError(w, r, http.StatusBadRequest, "Can't unmarshal request body")
		return
	}

	// Calculate an identifier for the test:
	testUUID, err := uuid.NewRandom()
	if err != nil {
		log.WithError(err).Error("Can't generate test identifier")
		sendError(w, r, http.StatusInternalServerError, "Can't generate test identifier")
		return
	}
	testID := testUUID.String()
	log.Infof("Assigned test identifier '%s'", testID)

	// Create the test directory:
	testDir := filepath.Join(h.work, testID)
	err = os.Mkdir(testDir, 0700)
	if err != nil {
		log.Errorf("Can't create directory for test '%s': %v", testID, err)
		sendError(w, r, http.StatusInternalServerError, "Can't generate test directory")
		return
	}
	log.Infof("Created test directory '%s' for test '%d'", testDir, testID)

	// Write the binary to the test directory:
	testBinary := filepath.Join(testDir, "binary")
	err = ioutil.WriteFile(testBinary, requestBody.Binary, 0700)
	if err != nil {
		log.Errorf(
			"Can't create binary file '%s' for test '%s'",
			testBinary, testID,
		)
		sendError(
			w, r,
			http.StatusInternalServerError,
			"Can't create test binary file",
		)
		return
	}
	log.Infof("Created binary file '%s' for test '%s'", testBinary, testID)

	// Create the standard output file:
	testOutPath := filepath.Join(testDir, "stdout")
	testOutFile, err := os.OpenFile(testOutPath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Errorf("Can't create out file '%s' for test '%s': %v", testOutPath, testID, err)
		sendError(w, r, http.StatusInternalServerError, "Can't create output file")
		return
	}
	closeOutFile := func() {
		err := testOutFile.Close()
		if err != nil {
			log.Errorf(
				"Can't close output file '%s' for test '%s': %v",
				testOutPath, testID, err,
			)
		}
	}
	defer closeOutFile()
	log.Infof("Created output file '%s' for test '%s'", testOutPath, testID)

	// Create the standard error file:
	testErrPath := filepath.Join(testDir, "stderr")
	testErrFile, err := os.OpenFile(testErrPath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Errorf(
			"Can't create errors file '%s' for test '%s': v",
			testErrPath, testID, err,
		)
		sendError(w, r, http.StatusInternalServerError, "Can't open standard error file")
		return
	}
	closeErrFile := func() {
		err := testErrFile.Close()
		if err != nil {
			log.Errorf(
				"Can't close errors file '%s' for test '%s': %v",
				testErrPath, testID, err,
			)
		}
	}
	defer closeErrFile()
	log.Infof("Created errors file '%s' for test '%s'", testErrPath, testID)

	// Prepare the environment variables for the test:
	testEnv := os.Environ()
	for name, value := range requestBody.Env {
		h.addEnv(&testEnv, name, value)
	}

	// Run the binary:
	testCommand := exec.Command(
		testBinary,
		requestBody.Args...,
	)
	testCommand.Env = testEnv
	testCommand.Stdout = testOutFile
	testCommand.Stderr = testErrFile
	err = testCommand.Run()
	testCode := 0
	if err != nil {
		testStatus, ok := err.(*exec.ExitError)
		if ok {
			testCode = testStatus.ExitCode()
		} else {
			log.Errorf("Can't execute test binary for test '%s': %v", testID, err)
			sendError(w, r, http.StatusInternalServerError, "Can't execute test binary")
			return
		}
	}
	log.Infof("Test binary for test '%s' finished with exit code %d", testID, testCode)

	// Read the standard output file:
	testOut, err := ioutil.ReadFile(testOutPath)
	if err != nil {
		log.Errorf(
			"Can't read output file '%s' for test '%s': %v",
			testOutPath, testID, err,
		)
		sendError(w, r, http.StatusInternalServerError, "Can't read output file")
		return
	}

	// Read the standard error file:
	testErr, err := ioutil.ReadFile(testErrPath)
	if err != nil {
		log.Errorf(
			"Can't read errors file '%s' for test '%s': %v",
			testErrPath, testID, err,
		)
		sendError(w, r, http.StatusInternalServerError, "Can't read errors file")
		return
	}

	// Send the response:
	responseBody := &api.Test{
		Out:  testOut,
		Err:  testErr,
		Code: testCode,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	responseEncoder := json.NewEncoder(w)
	responseEncoder.SetIndent("", "  ")
	err = responseEncoder.Encode(responseBody)
	if err != nil {
		log.Errorf("Can't send response body for test '%s'", testID)
		return
	}
}

func (h *postTestHandler) addEnv(env *[]string, name, value string) {
	*env = append(*env, fmt.Sprintf("%s=%s", name, value))
}
