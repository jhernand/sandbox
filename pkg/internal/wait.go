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

// This file contains methods of the sandbox that are useful for waiting for different kinds of
// objects to be ready.

package internal

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/pointer"
)

// WaitForPod waits till the given pod is ready. It returns the description of the pod contained
// in the event that indicated that it is ready, or an error if something fails while checking or
// if the pod isn't ready after one minute.
func WaitForPod(client *corev1client.CoreV1Client, project, name string) (pod *corev1.Pod,
	err error) {
	log.Debugf("Waiting for pod '%s' to be ready", name)
	wtch, err := client.Pods(project).Watch(metav1.ListOptions{
		TimeoutSeconds: pointer.Int64Ptr(60),
	})
	if err != nil {
		return
	}
	channel := wtch.ResultChan()
	for event := range channel {
		log.Debugf("Received '%s' event for pod '%s'", event.Type, name)
		switch event.Type {
		case watch.Added, watch.Modified:
			tmp, ok := event.Object.(*corev1.Pod)
			if !ok {
				log.Errorf(
					"Unknown type of object '%T' while waiting for pod '%s' "+
						"to be ready, will ignore it",
					event.Object, name,
				)
				continue
			}
			if isPodReady(tmp) {
				log.Debugf("Pod '%s' is ready now", name)
				wtch.Stop()
				pod = tmp
				break
			}
		case watch.Deleted:
			wtch.Stop()
			err = fmt.Errorf(
				"pod '%s' was deleted while waiting for it to be ready",
				name,
			)
			return
		case watch.Error:
			wtch.Stop()
			err = fmt.Errorf(
				"unpexected error while waiting for pod '%s' to be ready: %v",
				name, event.Object,
			)
			return
		default:
			log.Errorf(
				"Unknown type of event '%s' while waiting for pod '%s' to be "+
					"ready, will ignore it",
				event.Type, name,
			)
			continue
		}
	}
	return
}

// isPodReady checks if the given pod is ready.
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		ready := condition.Type == corev1.PodReady
		status := condition.Status == corev1.ConditionTrue
		if ready && status {
			return true
		}
	}
	return false
}

// WaitForRoute waits till the given route is admitted. It returns the description of the route
// contained in the event that indicates that it was admitted, or an error if something fails while
// checking or the route isn't ready after waiting more than one minute.
func WaitForRoute(client *routev1client.RouteV1Client, project, name string) (route *routev1.Route, err error) {
	log.Debugf("Waiting for route '%s' to be admitted", name)
	wtch, err := client.Routes(project).Watch(metav1.ListOptions{
		TimeoutSeconds: pointer.Int64Ptr(60),
	})
	if err != nil {
		return
	}
	channel := wtch.ResultChan()
	for event := range channel {
		log.Debugf("Received '%s' event for route '%s'", event.Type, name)
		switch event.Type {
		case watch.Added, watch.Modified:
			tmp, ok := event.Object.(*routev1.Route)
			if !ok {
				log.Errorf(
					"Unknown type of object '%T' while waiting for route '%s' "+
						"to be admitted, will ignore it",
					event.Object, name,
				)
				continue
			}
			if isRouteAdmitted(tmp) {
				log.Debugf("Route '%s' is admitted now", name)
				wtch.Stop()
				route = tmp
				break
			}
		case watch.Deleted:
			wtch.Stop()
			err = fmt.Errorf(
				"route '%s' was deleted while waiting for it to be admitted",
				name,
			)
			return
		case watch.Error:
			wtch.Stop()
			err = fmt.Errorf(
				"unpexected error while waiting for route '%s' to be admitted: %v",
				name, event.Object,
			)
			return
		default:
			log.Errorf(
				"Unknown type of event '%s' while waiting for route '%s' to be "+
					"admitted, will ignore it",
				event.Type, name,
			)
			continue
		}
	}
	return
}

// isRouteAdmitted checks if the given route is admitted. The route is considered admitted when
// all the ingresses are admitted.
func isRouteAdmitted(route *routev1.Route) bool {
	result := true
	for _, ingress := range route.Status.Ingress {
		if !isIngressAdmitted(&ingress) {
			result = false
			break
		}
	}
	return result
}

// isIngressAdmitted checks if the given ingress is admitted.
func isIngressAdmitted(ingress *routev1.RouteIngress) bool {
	for _, condition := range ingress.Conditions {
		ready := condition.Type == routev1.RouteAdmitted
		status := condition.Status == corev1.ConditionTrue
		if ready && status {
			return true
		}
	}
	return false
}

// WaitForServer waits till the given backend server is responding with an status code different to
// 503, as that indicates that it is the actual backend server and not the OpenShift router that is
// responding.
func WaitForServer(client *http.Client, address string) error {
	for i := 0; i < 60; i++ {
		result, err := isServerResponding(client, address)
		if err != nil {
			return err
		}
		if result {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("backend '%s' isn't responding after one minute", address)
}

// isServerResponding checks if the given backend server is responding with an status code other
// different to 503.
func isServerResponding(client *http.Client, address string) (result bool, err error) {
	log.Debugf("Checking if server '%s' is responding", address)
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return
	}
	response, err := client.Do(request)
	if err != nil {
		log.Debugf("Server '%s' isn't responding: %v", address, err)
		result = false
		err = nil
		return
	}
	clean := func() {
		err := response.Body.Close()
		if err != nil {
			log.Errorf("Can't close response body: %v", err)
		}
	}
	defer clean()
	log.Debugf("Server '%s' responded with status code %d", address, response.StatusCode)
	result = response.StatusCode != http.StatusServiceUnavailable
	return
}

// WaitForDB waits till the given database server is responding.
func WaitForDB(source *url.URL) error {
	for i := 0; i < 60; i++ {
		result, err := isDBResponding(source)
		if err != nil {
			return err
		}
		if result {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("database '%s' isn't responding after one minute", source.String())
}

// isDBResponding checks if the given database server is responding.
func isDBResponding(source *url.URL) (result bool, err error) {
	log.Infof("Checking if database '%s' is responding", source.Host)
	db, err := sql.Open(source.Scheme, source.String())
	if err != nil {
		return
	}
	closer := func() {
		err := db.Close()
		if err != nil {
			log.Errorf("Can't close database '%s'", source.Host)
		}
	}
	defer closer()
	err = db.Ping()
	if err != nil {
		result = false
		err = nil
		return
	}
	log.Infof("Database '%s' responded", source.Host)
	result = true
	return
}
