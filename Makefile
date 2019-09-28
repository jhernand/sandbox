#
# Copyright (c) 2019 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# Ensure Go modules are enabled:
export GO111MODULE=on

# Image details:
image_registry:=quay.io
image_repository:=jhernand/sandbox
image_tag:=latest

# Flags for the sandbox:
sandbox_flags:=

# This flags indicates if the projects created during tests shouln't be removed
# when the tests finish:
keep:=false

.PHONY: cmds
cmds:
	for cmd in $$(ls cmd); do \
		CGO_ENABLED=0 \
		go build -o "$${cmd}" "./cmd/$${cmd}" || exit 1; \
	done

.PHONY: image
image: cmds
	podman build -t "$(image_registry)/$(image_repository):$(image_tag)" .

.PHONY: push
push: image
	podman push "$(image_registry)/$(image_repository):$(image_tag)"

.PHONY: tests
tests: cmds
	./sandbox runner $(sandbox_flags) tests/database

.PHONY: fmt
fmt:
	gofmt -s -l -w pkg cmd

.PHONY: lint
lint:
	golangci-lint run
