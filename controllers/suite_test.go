/*
Copyright 2020 The Flux authors

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

package controllers

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"helm.sh/helm/v3/pkg/getter"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var k8sManager ctrl.Manager
var testEnv *envtest.Environment
var storage *Storage
var externalEventsMock *mockEventRecorder

var examplePublicKey []byte
var examplePrivateKey []byte
var exampleCA []byte

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")
	t := true
	if os.Getenv("TEST_USE_EXISTING_CLUSTER") == "true" {
		testEnv = &envtest.Environment{
			UseExistingCluster: &t,
		}
	} else {
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
		}
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = sourcev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = sourcev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = sourcev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	Expect(loadExampleKeys()).To(Succeed())

	tmpStoragePath, err := ioutil.TempDir("", "source-controller-storage-")
	Expect(err).NotTo(HaveOccurred(), "failed to create tmp storage dir")

	storage, err = NewStorage(tmpStoragePath, "localhost", time.Second*30)
	Expect(err).NotTo(HaveOccurred(), "failed to create tmp storage")

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	externalEventsMock = &mockEventRecorder{metadatas: []map[string]string{}}

	err = (&GitRepositoryReconciler{
		Client:                k8sManager.GetClient(),
		Log:                   ctrl.Log.WithName("controllers").WithName(sourcev1.GitRepositoryKind),
		Scheme:                scheme.Scheme,
		Storage:               storage,
		ExternalEventRecorder: externalEventsMock,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred(), "failed to setup GtRepositoryReconciler")

	err = (&HelmRepositoryReconciler{
		Client:  k8sManager.GetClient(),
		Log:     ctrl.Log.WithName("controllers").WithName(sourcev1.HelmRepositoryKind),
		Scheme:  scheme.Scheme,
		Storage: storage,
		Getters: getter.Providers{getter.Provider{
			Schemes: []string{"http", "https"},
			New:     getter.NewHTTPGetter,
		}},
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred(), "failed to setup HelmRepositoryReconciler")

	err = (&HelmChartReconciler{
		Client:  k8sManager.GetClient(),
		Log:     ctrl.Log.WithName("controllers").WithName(sourcev1.HelmChartKind),
		Scheme:  scheme.Scheme,
		Storage: storage,
		Getters: getter.Providers{getter.Provider{
			Schemes: []string{"http", "https"},
			New:     getter.NewHTTPGetter,
		}},
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred(), "failed to setup HelmChartReconciler")

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient = k8sManager.GetClient()
	Expect(k8sClient).ToNot(BeNil())

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if storage != nil {
		err := os.RemoveAll(storage.BasePath)
		Expect(err).NotTo(HaveOccurred())
	}
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

func init() {
	rand.Seed(time.Now().UnixNano())
}

func loadExampleKeys() (err error) {
	examplePublicKey, err = ioutil.ReadFile("testdata/certs/server.pem")
	if err != nil {
		return err
	}
	examplePrivateKey, err = ioutil.ReadFile("testdata/certs/server-key.pem")
	if err != nil {
		return err
	}
	exampleCA, err = ioutil.ReadFile("testdata/certs/ca.pem")
	return err
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz1234567890")

func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

type mockEventRecorder struct {
	metadatas []map[string]string
}

func (m *mockEventRecorder) Eventf(
	o corev1.ObjectReference,
	metadata map[string]string,
	severity, reason string,
	messageFmt string, args ...interface{}) error {
	m.metadatas = append(m.metadatas, metadata)
	return nil
}

func (m *mockEventRecorder) Reset() {
	m.metadatas = []map[string]string{}
}

func includeMetadata(expected map[string]string) types.GomegaMatcher {
	return &includeMetadataMatcher{
		expected: expected,
	}
}

type includeMetadataMatcher struct {
	expected map[string]string
}

func (matcher *includeMetadataMatcher) Match(actual interface{}) (success bool, err error) {
	recorder, ok := actual.(*mockEventRecorder)
	if !ok {
		return false, fmt.Errorf("includeMetadata matcher expects a *mockEventRecorder")
	}

	for i := range recorder.metadatas {
		if reflect.DeepEqual(recorder.metadatas[i], matcher.expected) {
			return true, nil
		}
	}
	return false, nil
}

func (matcher *includeMetadataMatcher) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%#v\nto contain Metadata\n\t%#v", actual, matcher.expected)
}

func (matcher *includeMetadataMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%#v\nnot to contain the Metadata\n\t%#v", actual, matcher.expected)
}
