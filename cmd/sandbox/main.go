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

// This file contains the main program for the testing runner. The runner will
// run inside the temporary namespace created for the project and will receive
// via HTTP the binary to run and the command line arguments. It will then
// execute it and return the results.

package main

import (
	"flag"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/jhernand/sandbox/cmd/sandbox/cleaner"
	"github.com/jhernand/sandbox/cmd/sandbox/runner"
	"github.com/jhernand/sandbox/cmd/sandbox/server"
	log "github.com/sirupsen/logrus"
)

var args struct {
	debug bool
}

var root = &cobra.Command{
	Use:              "sandbox",
	Long:             "Test sandbox.",
	PersistentPreRun: run,
}

func init() {
	// Register the options that are managed by the 'flag' package, so that they will also be parsed
	// by the 'pflag' package:
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	// Add flags common to all the commands:
	flags := root.PersistentFlags()
	flags.BoolVar(
		&args.debug,
		"debug",
		false,
		"Enable debug mode.",
	)

	// Register the sub-commands:
	root.AddCommand(runner.Cmd)
	root.AddCommand(server.Cmd)
	root.AddCommand(cleaner.Cmd)
}

func run(cmd *cobra.Command, argv []string) {
	// Enable debug level if requested:
	if args.debug {
		log.SetLevel(log.DebugLevel)
	}
}

func main() {
	err := root.Execute()
	if err != nil {
		log.Errorf("Can't execute root command: %v", err)
		os.Exit(1)
	}
}
