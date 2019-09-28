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

package runner

import (
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/client-go/util/homedir"

	"github.com/jhernand/sandbox/pkg/runner"
)

var args struct {
	config    string
	proxy     string
	insecure  bool
	compile   bool
	recursive bool
	keep      bool
}

var Cmd = &cobra.Command{
	Use:   "runner [DIRECTORY]...",
	Short: "Runs a collection of tests inside an OpenShift project",
	Long:  "Runs a collection of tests inside an OpenShift project.",
	Run:   run,
}

func init() {
	// Calculate the default value for the configuration file command line flag:
	configDefault := ""
	homeDir := homedir.HomeDir()
	if homeDir != "" {
		configDefault = filepath.Join(homeDir, ".kube", "config")
	}

	// Define the command line flags:
	flags := Cmd.Flags()
	flags.StringVar(
		&args.config,
		"config",
		configDefault,
		"OpenShift client configuration file.",
	)
	flags.StringVar(
		&args.proxy,
		"proxy",
		"",
		"URL of the proxy server to use to connect to the OpenShift API.",
	)
	flags.BoolVar(
		&args.insecure,
		"insecure",
		false,
		"Indicates if connections to HTTPS servers that identify themselves with "+
			"certificates signed by unknown certificate authorities should "+
			"be accepted.",
	)
	flags.BoolVar(
		&args.recursive,
		"recursive",
		false,
		"Recursively find all directories that contain test files and run then.",
	)
	flags.BoolVar(
		&args.compile,
		"compile",
		true,
		"Compile the test binaries. If this is 'true' then the runner will run the "+
			"'go test -c ...' command for each directory containing test files. "+
			"Otherwise only the previously built test binaries will be used. This is "+
			"intended for situations where you want or need to compile the test "+
			"binaries with additional options that aren't supported by the runner.",
	)
	flags.BoolVar(
		&args.keep,
		"keep",
		false,
		"By default the runner will delete the OpenShift project that it creates to run "+
			"the tests. If this is set to 'true' then the OpenShift project will be "+
			"preserved.",
	)
}

func execute(cmd *cobra.Command, argv []string) int {
	// Check the command line:
	if len(argv) == 0 {
		log.Error("Expected at least one test to run")
		return 1
	}

	// Create the runner:
	rnnr, err := runner.NewRunner().
		Config(args.config).
		Proxy(args.proxy).
		Insecure(args.insecure).
		Keep(args.keep).
		Compile(args.compile).
		Recursive(args.recursive).
		Directories(argv...).
		Build()
	if err != nil {
		log.Errorf("Can't create runner: %v", err)
		return 1
	}

	// Remember to destroy the runner:
	destroy := func() {
		err := rnnr.Destroy()
		if err != nil {
			log.Errorf("Can't destroy runner: %v", err)
		}
	}
	defer destroy()

	// Run the tests:
	failed, err := rnnr.Run()
	if err != nil {
		log.Errorf("Can't run tests: %v", err)
		return 1
	}
	if failed > 0 {
		log.Info("Tests failed")
		return 1
	}
	log.Infof("Tests passed")
	return 0
}

func run(cmd *cobra.Command, argv []string) {
	code := execute(cmd, argv)
	os.Exit(code)
}
