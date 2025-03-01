/*
Copyright 2020 The Kubernetes Authors.

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

package main

import (
	"os"

	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/metrics/prometheus/clientgo" // for rest client metric registration
	_ "k8s.io/component-base/metrics/prometheus/version"  // for version metric registration
	"k8s.io/kubernetes/cmd/kube-scheduler/app"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/networkaware/networkoverhead"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/networkaware/topologicalsort"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/noderesources"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/noderesourcetopology"

	// Ensure scheme package is initialized.
	_ "github.com/elevated-systems/compute-gardener-scheduler/apis/config/scheme"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(networkoverhead.Name, networkoverhead.New),
		app.WithPlugin(topologicalsort.Name, topologicalsort.New),
		app.WithPlugin(noderesources.AllocatableName, noderesources.NewAllocatable),
		app.WithPlugin(noderesourcetopology.Name, noderesourcetopology.New),
		app.WithPlugin(computegardener.Name, computegardener.New),
	)

	code := cli.Run(command)
	os.Exit(code)
}
