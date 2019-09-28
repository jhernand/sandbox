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

// This file contains the implementation of the test runner.

package runner

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	projectv1 "github.com/openshift/api/project/v1"
	routev1 "github.com/openshift/api/route/v1"
	projectv1client "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	routev1client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacv1client "k8s.io/client-go/kubernetes/typed/rbac/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/jhernand/sandbox/pkg/api"
	"github.com/jhernand/sandbox/pkg/internal"
)

// RunnerBuilder contains the information and logic needed to create a test runner. Don't create
// instances of this type directly; use the NewRunner function instead.
type RunnerBuilder struct {
	// Compilation options:
	compile   bool
	recursive bool
	dirs      []string

	// Details to connect to the OpenShift API:
	config   string
	proxy    string
	insecure bool

	// Name of the OpenShift project:
	project string

	// Kubernetes API clients:
	coreV1    *corev1client.CoreV1Client
	projectV1 *projectv1client.ProjectV1Client
	rbacV1    *rbacv1client.RbacV1Client
	routeV1   *routev1client.RouteV1Client

	// Details of the server:
	server *Server

	// Flag indicating if the OpenShift project should be preserved when the runner is destroyed:
	keep bool
}

// Runner is the test runner.
type Runner struct {
	// Compilation options:
	compile   bool
	recursive bool
	dirs      []string

	// Name of the OpenShift project:
	project string

	// Kubernetes API clients:
	projectV1 *projectv1client.ProjectV1Client

	// Details of the server:
	server *Server

	// Flag indicating if the OpenShift project should be preserved when the runner is destroyed:
	keep bool
}

// NewRunner creates a new object that knows how to build test runners.
func NewRunner() *RunnerBuilder {
	return &RunnerBuilder{
		compile:   true,
		recursive: false,
	}
}

// Config sets the configuration file that will be used to connect to the OpenShift API. If not set
// it will try to use the `~/.kube/config` or the configuration provided by the cluster to the pod.
func (b *RunnerBuilder) Config(value string) *RunnerBuilder {
	b.config = value
	return b
}

// Proxy sets the URL of the proxy server that will be used to connect to the OpenShift API.
func (b *RunnerBuilder) Proxy(value string) *RunnerBuilder {
	b.proxy = value
	return b
}

// Insecure indicates if connections to HTTPS servers that identify themselves with certificates
// signed by unknown certificate authorities should be accepted. The default is to not accept
// such connections.
func (b *RunnerBuilder) Insecure(value bool) *RunnerBuilder {
	b.insecure = value
	return b
}

// Compile indicates if the test binaries should be compiled. The default value is true.
func (b *RunnerBuilder) Compile(value bool) *RunnerBuilder {
	b.compile = value
	return b
}

// Recursive indicates if the given package names should be recursively scanned looking for all the
// test suites. The default value is false.
func (b *RunnerBuilder) Recursive(value bool) *RunnerBuilder {
	b.recursive = value
	return b
}

// Directory adds one directory to process.
func (b *RunnerBuilder) Directory(value string) *RunnerBuilder {
	b.dirs = append(b.dirs, value)
	return b
}

// Directories adds a collection of directories to process.
func (b *RunnerBuilder) Directories(values ...string) *RunnerBuilder {
	b.dirs = append(b.dirs, values...)
	return b
}

// Keep indicates if the OpenShift project should be preserved when the runner is destroyed.
func (b *RunnerBuilder) Keep(value bool) *RunnerBuilder {
	b.keep = value
	return b
}

// Build uses the information stored in the builder to create a new runner.
func (b *RunnerBuilder) Build() (rnnr *Runner, err error) {
	// Check parameters:
	if len(b.dirs) == 0 {
		err = fmt.Errorf("at least one directory must be provided")
		return
	}

	// Make a copy of the directories array:
	dirs := make([]string, len(b.dirs))
	copy(dirs, b.dirs)

	// If the configuration is then try to get it from the `~/.kube/config' file:
	configFile := b.config
	if configFile == "" {
		homeDir := homedir.HomeDir()
		if homeDir != "" {
			configFile = filepath.Join(homeDir, ".kube", "config")
			_, err = os.Stat(configFile)
			if os.IsNotExist(err) {
				configFile = ""
				err = nil
			}
			if err != nil {
				return
			}
		}
	}

	// Load the configuration either from the given configuration file or from the default
	// location used when running inside a cluster:
	restConfig, err := clientcmd.BuildConfigFromFlags("", configFile)
	if err != nil {
		return
	}

	// Configure the proxy:
	var proxy *url.URL
	if b.proxy != "" {
		proxy, err = url.Parse(b.proxy)
		if err != nil {
			return
		}
		restConfig.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
			t, ok := rt.(*http.Transport)
			if ok {
				t.Proxy = http.ProxyURL(proxy)
				return t
			} else {
				log.Errorf(
					"don't know how to configure proxy on round tripper of "+
						"type '%T'",
					rt,
				)
				return rt
			}
		}
	}

	// Create the Kubernetes clients:
	b.coreV1, err = corev1client.NewForConfig(restConfig)
	if err != nil {
		return
	}
	b.projectV1, err = projectv1client.NewForConfig(restConfig)
	if err != nil {
		return
	}
	b.rbacV1, err = rbacv1client.NewForConfig(restConfig)
	if err != nil {
		return
	}
	b.routeV1, err = routev1client.NewForConfig(restConfig)
	if err != nil {
		return
	}

	// Make sure that the project, the cleaner and the server exist:
	err = b.ensureProject()
	if err != nil {
		return
	}
	if !b.keep {
		err = b.ensureCleaner()
		if err != nil {
			return
		}
	}
	err = b.ensureServer()
	if err != nil {
		return
	}

	// Create and populate the runner object:
	rnnr = &Runner{
		compile:   b.compile,
		recursive: b.recursive,
		dirs:      dirs,
		keep:      b.keep,
		project:   b.project,
		projectV1: b.projectV1,
		server:    b.server,
	}

	return
}

// Destroy releases all the resources used by the runner.
func (r *Runner) Destroy() error {
	var err error

	// Delete the OpenShift project:
	if r.keep {
		log.Infof("Deleting project '%s'", r.project)
		err = r.projectV1.Projects().Delete(r.project, nil)
		if errors.IsNotFound(err) {
			err = nil
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// Run runs the tests and returns the of failed tests.
func (r *Runner) Run() (failed int, err error) {
	// Enrich the list of directories recursively looking for directories that contain test
	// files, if needed:
	if r.recursive {
		err = r.scanDirectories()
		if err != nil {
			return
		}
	}
	sort.Strings(r.dirs)

	// Dump the list of directories to process:
	if len(r.dirs) == 1 {
		log.Infof("Found one directory containing test files")
	} else {
		log.Infof("Found %d directories containing test files", len(r.dirs))
	}
	if log.IsLevelEnabled(log.DebugLevel) {
		for _, directory := range r.dirs {
			log.Debugf("Found directory '%s' containing test files", directory)
		}
	}

	// Compile the test binaries if needed:
	if r.compile {
		err = r.compileBinaries()
		if err != nil {
			return
		}
	}

	// Find the generated test binaries:
	binaries, err := filepath.Glob("*.test")
	if err != nil {
		return
	}
	sort.Strings(binaries)

	// Dump the list of binaries:
	if len(binaries) == 1 {
		log.Infof("Found one test binary")
	} else {
		log.Infof("Found %d test binaries", len(binaries))
	}
	if log.IsLevelEnabled(log.DebugLevel) {
		for _, binary := range binaries {
			log.Debugf("Found test binary '%s'", binary)
		}
	}

	// Send the binaries fo the server for execution:
	failed = 0
	for _, binary := range binaries {
		log.Infof("Running test binary '%s'", binary)
		var bytes []byte
		bytes, err = ioutil.ReadFile(binary)
		if err != nil {
			log.Errorf("Can't read test binary from file '%s': %v", binary, err)
			continue
		}
		var request *api.Test
		request = &api.Test{
			Binary: bytes,
		}
		var response *api.Test
		response, err = r.server.Send(request)
		if err != nil {
			log.Errorf("Can't send request for test binary '%s': %v", binary, err)
			continue
		}
		if response.Out != nil {
			log.Infof("Output of test binary '%s' follows", binary)
			_, _ = os.Stdout.Write(response.Out)
		} else {
			log.Infof("Test binary '%s' didnt' produce output", binary)
		}
		if response.Err != nil {
			log.Infof("Error output of test binary '%s' follows", binary)
			_, _ = os.Stderr.Write(response.Err)
		} else {
			log.Infof("Test binary '%s' didn't produce error output", binary)
		}
		log.Infof("Test binary '%s' finished with exit code %d", binary, response.Code)
		if response.Code != 0 {
			failed++
		}
	}

	return
}

// scanDirectories recursively scans the directories given by the caller, and adds the
// sub-directories that contain test files.
func (r *Runner) scanDirectories() error {
	set := map[string]bool{}
	for _, root := range r.dirs {
		log.Infof("Scanning directory '%s' for test files", root)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if info.Mode().IsRegular() && strings.HasSuffix(path, "_test.go") {
				set[filepath.Dir(path)] = true
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	count := len(set)
	r.dirs = make([]string, count)
	i := 0
	for directory := range set {
		r.dirs[i] = directory
		i++
	}
	return nil
}

// compileBinaries compiles the test binaries using the `go test -c ...` command.
func (r *Runner) compileBinaries() error {
	for _, directory := range r.dirs {
		log.Infof("Compiling test binary for directory '%s'", directory)
		pckg := directory
		if !strings.HasPrefix(directory, dotSeparator) {
			pckg = dotSeparator + directory
		}
		compileCmd := exec.Command("go", "test", "-c", pckg)
		compileCmd.Stdout = os.Stdout
		compileCmd.Stderr = os.Stderr
		if log.IsLevelEnabled(log.DebugLevel) {
			log.Debugf("Running command '%s'", strings.Join(compileCmd.Args, " "))
		}
		err := compileCmd.Run()
		if err != nil {
			compileStatus, ok := err.(*exec.ExitError)
			if ok {
				compileCode := compileStatus.ExitCode()
				err = fmt.Errorf(
					"compilation of tests binary for directory '%s' finished "+
						"with exist code %d",
					directory, compileCode,
				)
			}
			return err
		}
	}
	return nil
}

// ensureProject makes sure that the OpenShift project exists, creating it if needed.
func (b *RunnerBuilder) ensureProject() error {
	// Generate a name for the project:
	usr, err := user.Current()
	if err != nil {
		return err
	}
	b.project = fmt.Sprintf("sandbox-%s-%d", usr.Username, time.Now().Unix())

	// Create the project:
	log.Infof("Creating project '%s'", b.project)
	request := &projectv1.ProjectRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: b.project,
		},
	}
	_, err = b.projectV1.ProjectRequests().Create(request)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Create the service account that will be used to run the tests:
	account := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: serverApp,
		},
	}
	_, err = b.coreV1.ServiceAccounts(b.project).Create(account)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Give the service account full permissions inside the project:
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: serverApp,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "server",
				Namespace: b.project,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "admin",
		},
	}
	_, err = b.rbacV1.RoleBindings(b.project).Create(binding)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	return nil
}

// ensureCleaner makes sure that the cleaner exists, creating it if needed.
func (b *RunnerBuilder) ensureCleaner() error {
	var err error

	// Create the service account that will be used to run the cleaner:
	account := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: cleanerApp,
		},
	}
	_, err = b.coreV1.ServiceAccounts(b.project).Create(account)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Give the cleaner account full permissions inside the project:
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: cleanerApp,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      cleanerApp,
				Namespace: b.project,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "admin",
		},
	}
	_, err = b.rbacV1.RoleBindings(b.project).Create(binding)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Create the cleaner pod:
	labels := map[string]string{
		internal.AppLabel: cleanerApp,
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   cleanerApp,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: cleanerApp,
			Containers: []corev1.Container{
				{
					Name: cleanerApp,
					Command: []string{
						sandboxCommand,
						"cleaner",
						"--wait=1m",
					},
					Image:           sandboxImage,
					ImagePullPolicy: corev1.PullAlways,
				},
			},
		},
	}
	_, err = b.coreV1.Pods(b.project).Create(pod)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	return nil
}

// ensureServer makes sure that the server exists in the OpenShift project, creating it if needed.
func (b *RunnerBuilder) ensureServer() error {
	// Generate the random token that will be used to authenticate to the runner server:
	id, err := uuid.NewRandom()
	if err != nil {
		return err
	}
	token := id.String()

	// Create the service account that will be used to run the server:
	account := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: serverApp,
		},
	}
	_, err = b.coreV1.ServiceAccounts(b.project).Create(account)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Give the service account full permissions inside the project:
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: serverApp,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serverApp,
				Namespace: b.project,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "admin",
		},
	}
	_, err = b.rbacV1.RoleBindings(b.project).Create(binding)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Create the specifications of the volumes that will be used by the runner:
	workVolume := internal.EmptyDirVolume("work")

	// Create the server pod:
	podLabels := map[string]string{
		internal.AppLabel: serverApp,
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   serverApp,
			Labels: podLabels,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: serverApp,
			Volumes: []corev1.Volume{
				workVolume,
			},
			Containers: []corev1.Container{
				{
					Name: serverApp,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      workVolume.Name,
							MountPath: serverWork,
						},
					},
					Command: []string{
						sandboxCommand,
						"server",
						fmt.Sprintf(
							"--listen=%s:%d",
							serverAddress, serverPort,
						),
						fmt.Sprintf("--token=%s", token),
						fmt.Sprintf("--work=%s", serverWork),
					},
					Image:           sandboxImage,
					ImagePullPolicy: corev1.PullAlways,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: serverPort,
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
		},
	}
	_, err = b.coreV1.Pods(b.project).Create(pod)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Create the service:
	serviceLabels := map[string]string{
		internal.AppLabel: serverApp,
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   serverApp,
			Labels: serviceLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				internal.AppLabel: serverApp,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       serverPort,
					TargetPort: intstr.FromInt(serverPort),
				},
			},
		},
	}
	_, err = b.coreV1.Services(b.project).Create(service)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Create the route:
	routeLabels := map[string]string{
		internal.AppLabel: serverApp,
	}
	routeAnnotations := map[string]string{
		"haproxy.router.openshift.io/timeout": "10m",
	}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serverApp,
			Labels:      routeLabels,
			Annotations: routeAnnotations,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: serverApp,
			},
			TLS: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationEdge,
			},
		},
	}
	_, err = b.routeV1.Routes(b.project).Create(route)
	if errors.IsAlreadyExists(err) {
		err = nil
	}
	if err != nil {
		return err
	}

	// Wait till the server and the route are ready:
	pod, err = internal.WaitForPod(b.coreV1, b.project, serverApp)
	if err != nil {
		return err
	}
	route, err = internal.WaitForRoute(b.routeV1, b.project, serverApp)
	if err != nil {
		return err
	}

	// Now that the route is ready we can calculate the complete address of the server:
	address := fmt.Sprintf("https://%s", route.Spec.Host)

	// Create the HTTP client:
	transport := &http.Transport{}
	client := &http.Client{
		Transport: transport,
	}
	if b.proxy != "" {
		var proxyURL *url.URL
		proxyURL, err = url.Parse(b.proxy)
		if err != nil {
			return err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	if b.insecure {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: b.insecure,
		}
	}

	// Wait till the server is responding:
	err = internal.WaitForServer(client, address)
	if err != nil {
		return err
	}

	// Create and populate the object:
	b.server = &Server{
		token:   token,
		address: address,
		client:  client,
	}

	return nil
}

// Sandbox constants:
const (
	sandboxCommand = "/usr/local/bin/sandbox"
	sandboxImage   = "quay.io/jhernand/sandbox"
)

// Cleaner constants:
const (
	cleanerApp = "cleaner"
)

// Server constants:
const (
	serverApp     = "server"
	serverAddress = "0.0.0.0"
	serverPort    = 8000
	serverWork    = "/var/cache/sandbox"
)

// The `go test -c ...` command needs to see the `./` prefix in the package names to understand
// that they are relative:
var dotSeparator = fmt.Sprintf(".%c", filepath.Separator)
