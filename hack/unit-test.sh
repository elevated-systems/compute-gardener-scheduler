#!/usr/bin/env bash

# Copyright 2020 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE}")/..
source "${SCRIPT_ROOT}/hack/lib/init.sh"

kube::log::status "Configuring envtest"
TEMP_DIR=${TMPDIR-/tmp}
source "${TEMP_DIR}/setup-envtest"

# get the args to pass to go test
ARGS=("$@")

go test "${ARGS[@]}" \
  github.com/elevated-systems/compute-gardener-scheduler/cmd/... \
  github.com/elevated-systems/compute-gardener-scheduler/pkg/... \
  github.com/elevated-systems/compute-gardener-scheduler/apis/...
