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
	"context"
	"testing"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

type mockHandle struct {
	framework.Handle
	informerFactory informers.SharedInformerFactory
}

func (m *mockHandle) SharedInformerFactory() informers.SharedInformerFactory {
	return m.informerFactory
}

// testConfig implements runtime.Object
type testConfig struct {
	config.Config
}

func (c *testConfig) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (c *testConfig) DeepCopyObject() runtime.Object {
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}

func TestComputeGardenerPlugin(t *testing.T) {
	ctx := context.Background()

	// Create test configuration
	obj := &testConfig{
		Config: config.Config{
			Cache: config.APICacheConfig{
				Timeout:     time.Second,
				MaxRetries:  3,
				RetryDelay:  time.Second,
				RateLimit:   10,
				CacheTTL:    time.Minute,
				MaxCacheAge: time.Hour,
			},
			Carbon: config.CarbonConfig{
				Enabled:            true,
				Provider:           "electricity-maps-api",
				IntensityThreshold: 200,
				APIConfig: config.ElectricityMapsAPIConfig{
					APIKey: "test-key",
					Region: "test-region",
					URL:    "http://mock-url/",
				},
			},
			Scheduling: config.SchedulingConfig{
				MaxSchedulingDelay: 24 * time.Hour,
			},
			Power: config.PowerConfig{
				DefaultIdlePower: 100.0,
				DefaultMaxPower:  400.0,
				NodePowerConfig: map[string]config.NodePower{
					"test-node": {
						IdlePower: 50.0,
						MaxPower:  200.0,
					},
				},
			},
		},
	}

	// Create mock handle with informer factory
	client := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	handle := &mockHandle{
		informerFactory: informerFactory,
	}

	// Start informers
	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	// Create plugin
	plugin, err := computegardener.New(ctx, obj, handle)
	if err != nil {
		t.Fatalf("Failed to create ComputeGardenerScheduler plugin: %v", err)
	}
	if plugin == nil {
		t.Fatal("Plugin is nil")
	}

	// Verify plugin name
	if name := plugin.Name(); name != computegardener.Name {
		t.Errorf("Expected plugin name %s, got %s", computegardener.Name, name)
	}

	// Verify plugin implements PreFilter interface
	if _, ok := plugin.(framework.PreFilterPlugin); !ok {
		t.Error("Plugin does not implement PreFilterPlugin interface")
	}

	// Verify plugin can be initialized
	if err := plugin.(*computegardener.ComputeGardenerScheduler).PreFilterExtensions(); err != nil {
		t.Errorf("PreFilterExtensions() returned unexpected error: %v", err)
	}
}
