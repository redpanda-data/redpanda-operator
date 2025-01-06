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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	cmapiv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	helmControllerAPIV2Beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	helmControllerAPIV2Beta2 "github.com/fluxcd/helm-controller/api/v2beta2"
	fluxclient "github.com/fluxcd/pkg/runtime/client"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"github.com/redpanda-data/common-go/rpadmin"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"

	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/flux"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/vectorized"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	consolepkg "github.com/redpanda-data/redpanda-operator/operator/pkg/console"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	k8sClient             client.Client
	testEnv               *testutils.RedpandaTestEnv
	cfg                   *rest.Config
	testAdminAPI          *adminutils.MockAdminAPI
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

	err = scheme.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = vectorizedv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = redpandav1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = redpandav1alpha2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = cmapiv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = helmControllerAPIV2Beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = helmControllerAPIV2Beta2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = helmControllerAPIV2Beta2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = sourcev1beta2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = sourcev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Logger: l,
		Controller: config.Controller{
			MaxConcurrentReconciles: 2,
		},
	})
	Expect(err).ToNot(HaveOccurred())
	ctx = ctrl.SetupSignalHandler()
	ctx, controllerCancel = context.WithCancel(ctx)

	for _, controller := range flux.NewFluxControllers(k8sManager, fluxclient.Options{}, fluxclient.KubeConfigOptions{}) {
		Expect(controller.SetupWithManager(ctx, k8sManager)).ToNot(HaveOccurred())
	}

	testAdminAPI = &adminutils.MockAdminAPI{Log: l.WithName("testAdminAPI").WithName("mockAdminAPI")}
	testAdminAPIFactory = func(
		_ context.Context,
		_ client.Reader,
		_ *vectorizedv1alpha1.Cluster,
		_ string,
		_ types.AdminTLSConfigProvider,
		pods ...string,
	) (adminutils.AdminAPIClient, error) {
		if len(pods) == 1 {
			return &adminutils.NodePoolScopedMockAdminAPI{
				MockAdminAPI: testAdminAPI,
				Pod:          pods[0],
			}, nil
		}
		return testAdminAPI, nil
	}

	testStore = consolepkg.NewStore(k8sManager.GetClient(), k8sManager.GetScheme())
	testKafkaAdmin = &mockKafkaAdmin{}
	testKafkaAdminFactory = func(context.Context, client.Client, *vectorizedv1alpha1.Cluster, *consolepkg.Store) (consolepkg.KafkaAdminClient, error) {
		return testKafkaAdmin, nil
	}

	err = (&vectorized.ClusterReconciler{
		Client:                   k8sManager.GetClient(),
		Log:                      l.WithName("controllers").WithName("core").WithName("RedpandaCluster"),
		Scheme:                   k8sManager.GetScheme(),
		AdminAPIClientFactory:    testAdminAPIFactory,
		DecommissionWaitInterval: 100 * time.Millisecond,
	}).WithClusterDomain("cluster.local").WithConfiguratorSettings(resources.ConfiguratorSettings{
		ConfiguratorBaseImage: "vectorized/configurator",
		ConfiguratorTag:       "latest",
		ImagePullPolicy:       "Always",
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	driftCheckPeriod := 500 * time.Millisecond
	err = (&vectorized.ClusterConfigurationDriftReconciler{
		Client:                k8sManager.GetClient(),
		Log:                   l.WithName("controllers").WithName("core").WithName("RedpandaCluster"),
		Scheme:                k8sManager.GetScheme(),
		AdminAPIClientFactory: testAdminAPIFactory,
		DriftCheckPeriod:      &driftCheckPeriod,
	}).WithClusterDomain("cluster.local").SetupWithManager(k8sManager)
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
	err = (&redpanda.RedpandaReconciler{
		Client:        k8sManager.GetClient(),
		ClientFactory: internalclient.NewFactory(k8sManager.GetConfig(), k8sManager.GetClient()),
		Scheme:        k8sManager.GetScheme(),
		EventRecorder: k8sManager.GetEventRecorderFor("RedpandaReconciler"),
	}).SetupWithManager(ctx, k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&redpanda.DecommissionReconciler{
		Client:       k8sManager.GetClient(),
		OperatorMode: false,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&redpanda.RedpandaNodePVCReconciler{
		Client:       k8sManager.GetClient(),
		OperatorMode: false,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&redpanda.ManagedDecommissionReconciler{
		Client: k8sManager.GetClient(),
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

var _ = BeforeEach(func() {
	By("Cleaning the admin API")
	testAdminAPI.Clear()
	// Register some known properties for all tests
	testAdminAPI.RegisterPropertySchema("auto_create_topics_enabled", rpadmin.ConfigPropertyMetadata{NeedsRestart: false})
	testAdminAPI.RegisterPropertySchema("cloud_storage_segment_max_upload_interval_sec", rpadmin.ConfigPropertyMetadata{NeedsRestart: true})
	testAdminAPI.RegisterPropertySchema("log_segment_size", rpadmin.ConfigPropertyMetadata{NeedsRestart: true})
	testAdminAPI.RegisterPropertySchema("enable_rack_awareness", rpadmin.ConfigPropertyMetadata{NeedsRestart: false})

	// By default we set the following properties and they'll be loaded by redpanda from the .bootstrap.yaml
	// So we initialize the test admin API with those
	testAdminAPI.SetProperty("auto_create_topics_enabled", false)
	testAdminAPI.SetProperty("cloud_storage_segment_max_upload_interval_sec", 1800)
	testAdminAPI.SetProperty("log_segment_size", 536870912)
	testAdminAPI.SetProperty("enable_rack_awareness", true)
})

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
