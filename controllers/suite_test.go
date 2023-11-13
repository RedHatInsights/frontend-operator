package controllers

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	prom "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	v1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1"

	crd "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var k8sClient client.Client
var testEnv *envtest.Environment
var stopController context.CancelFunc
var MonitoringNamespace = "openshift-customer-monitoring"

func TestAPIs(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)

	ginkgo.RunSpecs(t, "Controller Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	k8sscheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(k8sscheme))
	utilruntime.Must(prom.AddToScheme(k8sscheme))
	utilruntime.Must(networking.AddToScheme(k8sscheme))

	ginkgo.By("bootstrapping test environment")
	// Here be dragons: env-test does not play nice with third party CRDs
	// https://github.com/kubernetes-sigs/controller-runtime/issues/1191#issuecomment-833058115
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "config", "crd", "test-resources"),
		},
		Scheme:                k8sscheme,
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(cfg).NotTo(gomega.BeNil())

	err = crd.AddToScheme(k8sscheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: k8sscheme})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(k8sClient).NotTo(gomega.BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: k8sscheme,
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = (&FrontendReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("CronJob"),
	}).SetupWithManager(k8sManager)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	ctx, cancel := context.WithCancel(context.Background())
	stopController = cancel

	monitorNs := v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: MonitoringNamespace,
		},
	}
	gomega.Expect(k8sClient.Create(ctx, &monitorNs)).Should(gomega.Succeed())

	go func() {
		err = k8sManager.Start(ctx)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}()

}, 60)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")
	stopController()
	err := testEnv.Stop()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
})
