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

// This file contains the implementation of the object that removes the project after waiting
// some time.

package cleaner

import (
	"fmt"
	"io/ioutil"
	"time"

	projectv1client "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
)

// CleanerBuilder contains the information and logic needed to create the cleaner. Don't create
// instances of this type directly; use the NewCleaner function instead.
type CleanerBuilder struct {
	wait time.Duration
}

// Cleaner is the implementation of the cleaner.
type Cleaner struct {
	wait    time.Duration
	api     *projectv1client.ProjectV1Client
	project string
	stop    chan bool
	clean   *time.Timer
}

// NewCleaner creates a new object that knows how to delete the OpenShift project.
func NewCleaner() *CleanerBuilder {
	return &CleanerBuilder{}
}

// Wait sets the time that the cleaner should wait before deleting the OpenShift project.
func (b *CleanerBuilder) Wait(value time.Duration) *CleanerBuilder {
	b.wait = value
	return b
}

// Build uses the information stored in the builder to create a new cleaner. Note that this will
// create the cleaner but will not start it. To start it use the Start method.
func (b *CleanerBuilder) Build() (c *Cleaner, err error) {
	// Check parameters:
	if b.wait == 0 {
		err = fmt.Errorf("wait time can't be zero")
		return
	}

	// Get the name of the project from the file where the cluster writes it:
	data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return
	}
	project := string(data)

	// Try to load the configuration assuming that the cleaner is running inside a pod:
	config, err := rest.InClusterConfig()
	if err != nil {
		return
	}

	// Create the client for the projects API:
	api, err := projectv1client.NewForConfig(config)
	if err != nil {
		return
	}

	// Create and populate the object:
	c = &Cleaner{
		wait:    b.wait,
		api:     api,
		project: project,
	}

	return
}

// Start starts the cleaner. This will wait the time given in the configuration and then will
// delete the project.
func (c *Cleaner) Start() error {
	// Create stop channel:
	c.stop = make(chan bool)

	// Create the clean timer:
	c.clean = time.NewTimer(c.wait)

	// Wait for the signals to stop or clean:
	go func() {
		select {
		case <-c.stop:
			c.clean.Stop()
		case <-c.clean.C:
			c.do()
		}
	}()

	return nil
}

// Stop stops the the cleaner. This will cancel the deletion of the project, if it didn't
// happen already.
func (c *Cleaner) Stop() error {
	c.stop <- true
	return nil
}

// Destroy releases all the resources used by the cleaner.
func (c *Cleaner) Destroy() error {
	close(c.stop)
	return nil
}

func (c *Cleaner) do() {
	log.Infof("Deleting project '%s'", c.project)
	options := &metav1.DeleteOptions{
		GracePeriodSeconds: pointer.Int64Ptr(1),
	}
	err := c.api.Projects().Delete(c.project, options)
	if err != nil {
		log.Errorf("Can't delete project '%s'", c.project)
		return
	}
	log.Infof("Project '%s' has been deleted", c.project)
}
