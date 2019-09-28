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

package cleaner

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/jhernand/sandbox/pkg/cleaner"
)

var args struct {
	wait time.Duration
}

var Cmd = &cobra.Command{
	Use:   "cleaner",
	Short: "Removes the OpenShift project after a given amount of time",
	Long:  "Removes the OpenShift project after a given amount of time.",
	Run:   run,
}

func init() {
	// Define the command line flags:
	flags := Cmd.Flags()
	flags.DurationVar(
		&args.wait,
		"wait",
		0,
		"How long to wait before remofing the project.",
	)
}

func execute(cmd *cobra.Command, argv []string) int {
	// Check the command line:
	if args.wait == 0 {
		log.Errorf("Option --wait is mandatory")
		return 1
	}

	// Create a channel to receive stop signals:
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	signal.Notify(signals, syscall.SIGINT)

	// Create the cleaner:
	clnr, err := cleaner.NewCleaner().
		Wait(args.wait).
		Build()
	if err != nil {
		log.Errorf("Can't create cleaner: %v", err)
		return 1
	}

	// Remember to destroy the cleaner:
	stop := func() {
		err := clnr.Stop()
		if err != nil {
			log.Errorf("Can't stop cleaner: %v", err)
		}
	}
	defer stop()
	destroy := func() {
		err := clnr.Destroy()
		if err != nil {
			log.Errorf("Can't destroy cleaner: %v", err)
		}
	}
	defer destroy()

	// Start the cleaner:
	err = clnr.Start()
	if err != nil {
		log.Errorf("Can't start cleaner: %v", err)
		return 1
	}

	// Wait till we receive a stop signal:
	<-signals

	// Stop the cleaner:
	err = clnr.Stop()
	if err != nil {
		log.Errorf("Can't stop cleaner: %v", err)
		return 1
	}

	return 0
}

func run(cmd *cobra.Command, argv []string) {
	code := execute(cmd, argv)
	os.Exit(code)
}
