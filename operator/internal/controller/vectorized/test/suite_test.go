// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// nolint:testpackage // this name is ok
package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/redpanda-data/common-go/rpadmin"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/nodewatcher"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/olddecommission"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/vectorized"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	consolepkg "github.com/redpanda-data/redpanda-operator/operator/pkg/console"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	k8sClient             client.Client
	testEnv               *testutils.RedpandaTestEnv
	cfg                   *rest.Config
	testAdminAPI          func(string) *adminutils.MockAdminAPI
	testAdminAPIFactory   adminutils.NodePoolAdminAPIClientFactory
	testStore             *consolepkg.Store
	testKafkaAdmin        *mockKafkaAdmin
	testKafkaAdminFactory consolepkg.KafkaAdminClientFactory
	ts                    *httptest.Server

	ctx              context.Context
	controllerCancel context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func(suiteCtx SpecContext) {
	l := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.DebugLevel))
	logf.SetLogger(l)

	By("bootstrapping test environment")
	testEnv = &testutils.RedpandaTestEnv{}

	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open("testdata/metrics.golden.txt")
		Expect(err).NotTo(HaveOccurred())
		defer f.Close()

		_, err = io.Copy(w, f)
		Expect(err).NotTo(HaveOccurred())
	}))

	resources.UnderReplicatedPartitionsHostOverwrite = ts.Listener.Addr().String()

	var err error
	cfg, err = testEnv.StartRedpandaTestEnv(false)
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: controller.UnifiedScheme,
		Logger: l,
		Controller: config.Controller{
			MaxConcurrentReconciles: 2,
		},
	})
	Expect(err).ToNot(HaveOccurred())
	ctx = ctrl.SetupSignalHandler()
	ctx, controllerCancel = context.WithCancel(ctx)

	testAdminAPIs := make(map[string]*adminutils.MockAdminAPI)
	var mu sync.Mutex
	testAdminAPI = func(clusterName string) *adminutils.MockAdminAPI {
		mu.Lock()
		defer mu.Unlock()
		api, found := testAdminAPIs[clusterName]
		if !found {
			api = &adminutils.MockAdminAPI{Log: l.WithName("testAdminAPI").WithName("mockAdminAPI").WithName(clusterName)}

			api.Clear()

			// Register some known properties for all tests
			api.RegisterPropertySchema("auto_create_topics_enabled", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "boolean"})
			api.RegisterPropertySchema("cloud_storage_segment_max_upload_interval_sec", rpadmin.ConfigPropertyMetadata{NeedsRestart: true, Type: "integer"})
			api.RegisterPropertySchema("log_segment_size", rpadmin.ConfigPropertyMetadata{NeedsRestart: true, Type: "integer"})
			api.RegisterPropertySchema("enable_rack_awareness", rpadmin.ConfigPropertyMetadata{NeedsRestart: false, Type: "boolean"})

			// By default we set the following properties and they'll be loaded by redpanda from the .bootstrap.yaml
			// So we initialize the test admin API with those
			api.SetProperty("auto_create_topics_enabled", false)
			api.SetProperty("cloud_storage_segment_max_upload_interval_sec", 1800)
			api.SetProperty("log_segment_size", 536870912)
			api.SetProperty("enable_rack_awareness", true)

			testAdminAPIs[clusterName] = api
		}
		return api
	}

	testAdminAPIFactory = func(
		_ context.Context,
		_ client.Reader,
		cluster *vectorizedv1alpha1.Cluster,
		_ string,
		_ types.AdminTLSConfigProvider,
		_ redpanda.DialContextFunc,
		_ time.Duration,
		pods ...string,
	) (adminutils.AdminAPIClient, error) {
		api := testAdminAPI(fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name))
		if len(pods) == 1 {
			return &adminutils.NodePoolScopedMockAdminAPI{
				MockAdminAPI: api,
				Pod:          pods[0],
			}, nil
		}
		return api, nil
	}

	testStore = consolepkg.NewStore(k8sManager.GetClient(), k8sManager.GetScheme())
	testKafkaAdmin = &mockKafkaAdmin{}
	testKafkaAdminFactory = func(context.Context, client.Client, *vectorizedv1alpha1.Cluster, *consolepkg.Store) (consolepkg.KafkaAdminClient, error) {
		return testKafkaAdmin, nil
	}

	driftCheckPeriod := 500 * time.Millisecond
	err = (&vectorized.ClusterReconciler{
		Client:                         k8sManager.GetClient(),
		Log:                            l.WithName("controllers").WithName("core").WithName("RedpandaCluster"),
		Scheme:                         k8sManager.GetScheme(),
		AdminAPIClientFactory:          testAdminAPIFactory,
		DecommissionWaitInterval:       100 * time.Millisecond,
		ConfigurationReassertionPeriod: driftCheckPeriod,
	}).WithClusterDomain("cluster.local").WithConfiguratorSettings(resources.ConfiguratorSettings{
		ConfiguratorBaseImage: "redpanda-data/redpanda-operator",
		ConfiguratorTag:       "latest",
		ImagePullPolicy:       "Always",
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&vectorized.ConsoleReconciler{
		Client:                  k8sManager.GetClient(),
		Scheme:                  k8sManager.GetScheme(),
		Log:                     l.WithName("controllers").WithName("redpanda").WithName("Console"),
		AdminAPIClientFactory:   testAdminAPIFactory,
		Store:                   testStore,
		EventRecorder:           k8sManager.GetEventRecorderFor("Console"),
		KafkaAdminClientFactory: testKafkaAdminFactory,
	}).WithClusterDomain("cluster.local").SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	// Redpanda Reconciler
	err = (&redpandacontrollers.RedpandaReconciler{
		KubeConfig:    k8sManager.GetConfig(),
		Client:        k8sManager.GetClient(),
		ClientFactory: internalclient.NewFactory(k8sManager.GetConfig(), k8sManager.GetClient()),
		LifecycleClient: lifecycle.NewResourceClient(k8sManager, lifecycle.V2ResourceManagers(lifecycle.Image{
			Repository: "localhost/redpanda-operator",
			Tag:        "dev",
		}, lifecycle.CloudSecretsFlags{
			CloudSecretsEnabled: false,
		})),
		EventRecorder: k8sManager.GetEventRecorderFor("RedpandaReconciler"),
	}).SetupWithManager(ctx, k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&olddecommission.DecommissionReconciler{
		Client:       k8sManager.GetClient(),
		OperatorMode: false,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&nodewatcher.RedpandaNodePVCReconciler{
		Client:       k8sManager.GetClient(),
		OperatorMode: false,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()
	Expect(k8sManager.GetCache().WaitForCacheSync(context.Background())).To(BeTrue())

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())
}, NodeTimeout(20*time.Second))

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	// kube-apiserver hanging during cleanup
	// stopping the controllers prevents the hang
	controllerCancel()
	ts.Close()
	gexec.KillAndWait(5 * time.Second)
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
