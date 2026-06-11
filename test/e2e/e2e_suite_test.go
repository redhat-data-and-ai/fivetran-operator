/*
Copyright 2025.

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

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/redhat-data-and-ai/fivetran-operator/test/utils"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips CertManager installation during test setup.
	skipCertManagerInstall        = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	isCertManagerAlreadyInstalled = false

	projectImage = "example.com/fivetran-operator:v0.0.1"

	// Shared state across all test files
	controllerPodName string

	// E2E Fivetran configuration loaded from environment variables.
	// These are required for connector lifecycle tests.
	fivetranAPIKey    string
	fivetranAPISecret string
	fivetranGroupID   string
	googleSheetID     string
	googleNamedRange  string

	// Postgres RDS configuration for schema policy tests.
	postgresHost     string
	postgresPassword string
)

const (
	namespace              = "fivetran-operator"
	serviceAccountName     = "fivetran-operator-controller-manager"
	metricsServiceName     = "fivetran-operator-controller-manager-metrics-service"
	metricsRoleBindingName = "fivetran-operator-metrics-binding"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting fivetran-operator integration test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	loadE2EConfig()

	if !e2eConfigPresent() && os.Getenv("E2E_SKIP_LIFECYCLE") != "true" {
		Fail("Connector lifecycle env vars missing " +
			"(E2E_FIVETRAN_API_KEY, E2E_FIVETRAN_API_SECRET, E2E_FIVETRAN_GROUP_ID, " +
			"E2E_GOOGLE_SHEET_ID, E2E_GOOGLE_NAMED_RANGE). " +
			"Set E2E_SKIP_LIFECYCLE=true to skip intentionally.")
	}

	By("building the manager(Operator) image")
	cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectImage))
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager(Operator) image")

	By("loading the manager(Operator) image on Kind")
	err = utils.LoadImageToKindClusterWithName(projectImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager(Operator) image into Kind")

	if !skipCertManagerInstall {
		By("checking if cert manager is installed already")
		isCertManagerAlreadyInstalled = utils.IsCertManagerCRDsInstalled()
		if !isCertManagerAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Installing CertManager...\n")
			Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install CertManager")
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: CertManager is already installed. Skipping installation...\n")
		}
	}

	// --- Namespace ---
	By("creating manager namespace")
	cmd = exec.Command("kubectl", "create", "ns", namespace)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

	By("labeling the namespace to enforce the restricted security policy")
	cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
		"pod-security.kubernetes.io/enforce=restricted")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

	// --- Fivetran secrets & Vault ---
	// The operator pod requires fivetran-secrets to start (env var refs in the pod spec).
	// When lifecycle env vars are set, use real credentials and deploy a Vault dev server.
	// Otherwise, create placeholder secrets so the operator pod can still start for metrics tests.
	if e2eConfigPresent() {
		By("creating fivetran-secrets for the operator")
		_, _ = fmt.Fprintf(GinkgoWriter, "creating fivetran-secrets (credentials redacted from logs)\n")
		secretData := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]string{
				"name":      "fivetran-secrets",
				"namespace": namespace,
			},
			"type": "Opaque",
			"stringData": map[string]string{
				"FIVETRAN_API_KEY":    fivetranAPIKey,
				"FIVETRAN_API_SECRET": fivetranAPISecret,
			},
		}
		secretJSON, jsonErr := json.Marshal(secretData)
		Expect(jsonErr).NotTo(HaveOccurred(), "Failed to marshal fivetran-secrets")
		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = bytes.NewReader(secretJSON)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create fivetran-secrets")

		setupVaultDevServer()

		if postgresPassword != "" {
			By("storing Postgres password in Vault for schema policy tests")
			cmd = exec.Command("kubectl", "exec", "vault", "-n", vaultNamespace, "--",
				"vault", "kv", "put", "secret/e2e/postgres",
				fmt.Sprintf("password=%s", postgresPassword))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to store Postgres password in Vault")
		}
	} else {
		By("creating placeholder fivetran-secrets (lifecycle tests disabled)")
		placeholderSecret := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]string{
				"name":      "fivetran-secrets",
				"namespace": namespace,
			},
			"type": "Opaque",
			"stringData": map[string]string{
				"FIVETRAN_API_KEY":    "placeholder",
				"FIVETRAN_API_SECRET": "placeholder",
			},
		}
		placeholderJSON, jsonErr := json.Marshal(placeholderSecret)
		Expect(jsonErr).NotTo(HaveOccurred(), "Failed to marshal placeholder secrets")
		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = bytes.NewReader(placeholderJSON)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create placeholder fivetran-secrets")
	}

	// --- CRDs & Operator ---
	By("installing CRDs")
	cmd = exec.Command("make", "install")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("deploying the controller-manager")
	cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

	By("patching the deployment to use IfNotPresent image pull policy for Kind")
	cmd = exec.Command("kubectl", "patch", "deployment",
		"fivetran-operator-controller-manager", "-n", namespace,
		"--type=json",
		`-p=[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]`)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to patch imagePullPolicy")

	By("waiting for the controller-manager pod to be running")
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "get", "pods",
			"-l", "control-plane=controller-manager",
			"-o", "go-template={{ range .items }}"+
				"{{ if not .metadata.deletionTimestamp }}"+
				"{{ .metadata.name }}"+
				"{{ \"\\n\" }}{{ end }}{{ end }}",
			"-n", namespace)
		output, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		podNames := utils.GetNonEmptyLines(output)
		g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
		controllerPodName = podNames[0]

		cmd = exec.Command("kubectl", "get", "pods", controllerPodName,
			"-o", "jsonpath={.status.phase}", "-n", namespace)
		phase, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(phase).To(Equal("Running"), "controller-manager pod is not Running")
	}, 2*time.Minute, time.Second).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("cleaning up the curl pod for metrics")
	cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics",
		"-n", namespace, "--ignore-not-found=true")
	_, _ = utils.Run(cmd)

	By("undeploying the controller-manager")
	cmd = exec.Command("make", "undeploy")
	_, _ = utils.Run(cmd)

	By("uninstalling CRDs")
	cmd = exec.Command("make", "uninstall")
	_, _ = utils.Run(cmd)

	By("tearing down Vault dev server")
	teardownVaultDevServer()

	By("removing manager namespace")
	cmd = exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found=true")
	_, _ = utils.Run(cmd)

	if !skipCertManagerInstall && !isCertManagerAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling CertManager...\n")
		utils.UninstallCertManager()
	}
})
