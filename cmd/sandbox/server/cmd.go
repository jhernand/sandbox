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

package server

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/jhernand/sandbox/pkg/server"
)

var args struct {
	listen string
	token  string
	work   string
}

var Cmd = &cobra.Command{
	Use:   "server",
	Short: "Starts the sandbox server",
	Long: "Starts the sandbox server that runs inside the OpenShift project created to run " +
		"the tests.",
	Run: run,
}

func init() {
	flags := Cmd.Flags()
	flags.StringVar(
		&args.listen,
		"listen",
		defaultListen,
		fmt.Sprintf(
			"Address and port where the server will listen for requests.",
		),
	)
	flags.StringVar(
		&args.token,
		"token",
		"",
		fmt.Sprintf(
			"Authentication token that the server will require in every HTTP "+
				"request. This is mandatory and the server will fail to start "+
				"if it isn't specified.",
		),
	)
	flags.StringVar(
		&args.work,
		"work",
		"",
		fmt.Sprintf(
			"Working directory where the runner will create the subdirectories "+
				"needed for running the tests. If not specified it will use "+
				"the default temporary directory.",
		),
	)
}

func execute(cmd *cobra.Command, argv []string) int {
	// Check mandatory options:
	if args.token == "" {
		log.Errorf("Option '--token' is mandatory")
		return 1
	}

	// Create a channel to receive stop signals:
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	signal.Notify(signals, syscall.SIGINT)

	// Create the server:
	srvr, err := server.NewServer().
		Listen(args.listen).
		Token(args.token).
		Work(args.work).
		Build()
	if err != nil {
		log.Errorf("Can't create server: %v", err)
		return 1
	}
	destroy := func() {
		err := srvr.Destroy()
		if err != nil {
			log.Errorf("Can't destroy server: %v", err)
		}
	}
	defer destroy()

	// Start the server:
	err = srvr.Start()
	if err != nil {
		log.Errorf("Can't start server: %v", err)
		return 1
	}
	log.Infof("Server is now listening in address '%s'", args.listen)

	// Wait till we receive a stop signal:
	<-signals

	// Stop the server:
	err = srvr.Stop()
	if err != nil {
		log.Errorf("Can't stop server: %v", err)
		return 1
	}

	return 0
}

func run(cmd *cobra.Command, argv []string) {
	code := execute(cmd, argv)
	os.Exit(code)
}

// Default listen address:
const defaultListen = "0.0.0.0:8000"
