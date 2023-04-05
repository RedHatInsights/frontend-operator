/*
Copyright 2021 RedHatInsights.

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
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/go-logr/zapr"
	prom "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	cloudredhatcomv1alpha1 "github.com/RedHatInsights/frontend-operator/api/v1alpha1"
	"github.com/RedHatInsights/frontend-operator/operator"
	"github.com/RedHatInsights/rhc-osdk-utils/logging"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// Consts should be all caps. Go lint is wrong.
const (
	MetricsAddress   = ":8080"
	ProbeAddress     = ":8081"
	LeaderElect      = false
	LeaderElectionID = "1dd43857.cloud.redhat.com"
)

// I don't know what this method does
// I assume something in the k8s land calls this considering the linter
// isn't mad about it
func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(prom.AddToScheme(scheme))
	utilruntime.Must(cloudredhatcomv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// main is the entrypoint into the operator
func main() {
	metricsAddr, probeAddr, enableLeaderElection := parseArguments()

	logger := setupLogger()

	err := start(metricsAddr, probeAddr, enableLeaderElection)
	if err != nil {
		_ = logger.Sync()
		os.Exit(1)
	}
}

// newManager sets up a new controller manager
func newManager(metricsAddr, probeAddr string, enableLeaderElection bool) (manager.Manager, error) {
	return ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       LeaderElectionID,
	})
}

// parseArguments parses the command line arguments
func parseArguments() (string, string, bool) {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", MetricsAddress, "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ProbeAddress, "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", LeaderElect,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	return metricsAddr, probeAddr, enableLeaderElection
}

// register registers the frontend controller with the ControllerManager
func registerWithManager(mgr manager.Manager) error {
	controller := &operator.Controller{
		Log:    ctrl.Log.WithName("controllers").WithName("Frontend"),
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}

	return controller.RegisterWithManager(mgr)
}

// setupLogger sets up the logger for the operator
func setupLogger() *zap.Logger {
	logger, err := logging.SetupLogging(true)
	if err != nil {
		panic(err)
	}

	ctrl.SetLogger(zapr.NewLogger(logger))

	return logger
}

// start hooks the controller code up to the kubernetes API
func start(metricsAddr, probeAddr string, enableLeaderElection bool) error {
	mgr, err := newManager(metricsAddr, probeAddr, enableLeaderElection)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return fmt.Errorf("unable to start manager: %w", err)
	}

	err = registerWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Frontend")
		return fmt.Errorf("unable to create controller: %w", err)
	}
	//+kubebuilder:scaffold:builder

	err = mgr.AddHealthzCheck("healthz", healthz.Ping)
	if err != nil {
		setupLog.Error(err, "unable to set up health check")
		return fmt.Errorf("unable to setup health check: %w", err)
	}

	err = mgr.AddReadyzCheck("readyz", healthz.Ping)
	if err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return fmt.Errorf("unable to setup ready check: %w", err)
	}

	setupLog.Info("starting manager")
	err = mgr.Start(ctrl.SetupSignalHandler())
	if err != nil {
		setupLog.Error(err, "problem running manager")
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}
