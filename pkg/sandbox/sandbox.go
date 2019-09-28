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

// This file contains the implementation of the sandbox.

package sandbox

import (
	"io/ioutil"

	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacv1client "k8s.io/client-go/kubernetes/typed/rbac/v1"
	"k8s.io/client-go/rest"
)

// SandboxBuilder is an object that contains the data and the logic needed to build a sandbox
// environment. Do not create instances of this type directly, use the NewSandbox function instead.
type SandboxBuilder struct {
}

// Sandbox is the implementation of the sandbox.
type Sandbox struct {
	// Name of the OpenShift project:
	project string

	// Kubernetes API clients:
	coreV1 *corev1client.CoreV1Client
	rbacV1 *rbacv1client.RbacV1Client

	// Details of the database administrator:
	dbReady         bool
	dbAdminUser     string
	dbAdminPassword string
	dbAddress       string
}

// NewSandbox creates a new builder that knows how to create a sandbox. The sandbox will be created
// when eventually calling the Build method. The builder can be used multiple times to create
// multiple sandboxes.
func NewSandbox() *SandboxBuilder {
	return &SandboxBuilder{}
}

// Build uses the information stored inside the builder to create a new sandbox.
func (b *SandboxBuilder) Build() (s *Sandbox, err error) {
	// Get the name of the project from the file where the cluster writes it:
	data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return
	}
	project := string(data)

	// Load the configuration assuming that we are running inside a pod:
	config, err := rest.InClusterConfig()
	if err != nil {
		return
	}

	// Create the Kubernetes clients:
	coreV1, err := corev1client.NewForConfig(config)
	if err != nil {
		return
	}
	rbacV1, err := rbacv1client.NewForConfig(config)
	if err != nil {
		return
	}

	// Create and populate the sandbox:
	s = &Sandbox{
		project: project,
		coreV1:  coreV1,
		rbacV1:  rbacV1,
	}

	return
}

// Project returns the name of the OpenShift project.
func (s *Sandbox) Project() string {
	return s.project
}

// Destroy destroys the sandbox and all the associated resources.
func (s *Sandbox) Destroy() error {
	return nil
}
