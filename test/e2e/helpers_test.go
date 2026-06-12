/*
Copyright 2026.

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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/fivetran/go-fivetran/connections"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fivetranPkg "github.com/redhat-data-and-ai/fivetran-operator/pkg/fivetran"
	"github.com/redhat-data-and-ai/fivetran-operator/test/utils"
)

const vaultNamespace = "e2e-vault"

func loadE2EConfig() {
	fivetranAPIKey = os.Getenv("E2E_FIVETRAN_API_KEY")
	fivetranAPISecret = os.Getenv("E2E_FIVETRAN_API_SECRET")
	fivetranGroupID = os.Getenv("E2E_FIVETRAN_GROUP_ID")
	googleSheetID = os.Getenv("E2E_GOOGLE_SHEET_ID")
	googleNamedRange = os.Getenv("E2E_GOOGLE_NAMED_RANGE")
	postgresHost = os.Getenv("E2E_POSTGRES_HOST")
	postgresVaultPassword = os.Getenv("E2E_POSTGRES_PASSWORD")

	runSuffix = fmt.Sprintf("%x", time.Now().UnixNano()&0xFFFFFF)
}

func e2eConfigPresent() bool {
	return fivetranAPIKey != "" && fivetranAPISecret != "" && fivetranGroupID != "" &&
		googleSheetID != "" && googleNamedRange != ""
}

func postgresConfigPresent() bool {
	return e2eConfigPresent() && postgresHost != "" && postgresVaultPassword != ""
}

// testdataDir returns the absolute path to the testdata/ directory,
// resolved relative to this source file so it works regardless of cwd.
func testdataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata")
}

// loadTemplate parses a Go template from testdata/ and executes it with the given data.
func loadTemplate(name string, data any) string {
	tmpl, err := template.ParseFiles(filepath.Join(testdataDir(), name))
	Expect(err).NotTo(HaveOccurred(), "failed to parse template %s", name)
	var buf bytes.Buffer
	Expect(tmpl.Execute(&buf, data)).To(Succeed(), "failed to execute template %s", name)
	return buf.String()
}

// buildPostgresCR builds a Postgres connector CR YAML from the template.
// schemasBlock (if non-empty) is appended as the connectorSchemas spec.
func buildPostgresCR(crName, schemasBlock string) string {
	cr := loadTemplate("postgres_connector.yaml.tmpl", map[string]string{
		"Name":         crName,
		"Namespace":    namespace,
		"GroupID":      fivetranGroupID,
		"Host":         postgresHost,
		"SchemaPrefix": fmt.Sprintf("sp_%s_%s", strings.ReplaceAll(crName, "-", "_"), runSuffix),
	})
	if schemasBlock != "" {
		cr += "  connectorSchemas:\n" + schemasBlock
	}
	return cr
}

// buildGoogleSheetsCR builds a Google Sheets connector CR YAML from the template.
func buildGoogleSheetsCR(crName string) string {
	return loadTemplate("google_sheets_connector.yaml.tmpl", map[string]string{
		"Name":       crName,
		"Namespace":  namespace,
		"GroupID":    fivetranGroupID,
		"SheetID":    googleSheetID,
		"NamedRange": googleNamedRange,
		"Schema":     fmt.Sprintf("gs_%s_%s", strings.ReplaceAll(crName, "-", "_"), runSuffix),
	})
}

// buildOrphanCR builds an orphan (fivetran_log) connector CR YAML from the template.
func buildOrphanCR(crName string) string {
	return loadTemplate("orphan_connector.yaml.tmpl", map[string]string{
		"Name":      crName,
		"Namespace": namespace,
		"GroupID":   fivetranGroupID,
		"Schema":    fmt.Sprintf("or_%s_%s", strings.ReplaceAll(crName, "-", "_"), runSuffix),
	})
}

// getSchemaAnnotation returns the current schema-hash annotation value for a CR.
func getSchemaAnnotation(crName string) string {
	cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
		"-n", namespace, "-o",
		`jsonpath={.metadata.annotations.operator\.dataverse\.redhat\.com/schema-hash}`)
	output, err := utils.Run(cmd)
	if err != nil {
		return ""
	}
	return output
}

// setupVaultDevServer deploys a HashiCorp Vault dev server in a separate
// namespace and configures AppRole auth for the operator.
func setupVaultDevServer() {
	By("creating vault namespace")
	cmd := exec.Command("kubectl", "create", "ns", vaultNamespace)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create vault namespace")

	vaultManifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: vault
  namespace: %[1]s
  labels:
    app: vault
spec:
  containers:
  - name: vault
    image: hashicorp/vault:1.17
    command: ["vault", "server", "-dev", "-dev-root-token-id=e2e-root-token", "-dev-listen-address=0.0.0.0:8200"]
    ports:
    - containerPort: 8200
    readinessProbe:
      httpGet:
        path: /v1/sys/health
        port: 8200
      initialDelaySeconds: 1
      periodSeconds: 2
    env:
    - name: VAULT_ADDR
      value: "http://127.0.0.1:8200"
    - name: VAULT_TOKEN
      value: "e2e-root-token"
    - name: SKIP_SETCAP
      value: "true"
---
apiVersion: v1
kind: Service
metadata:
  name: vault
  namespace: %[1]s
spec:
  selector:
    app: vault
  ports:
  - port: 8200
    targetPort: 8200`, vaultNamespace)

	By("deploying Vault pod and service")
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(vaultManifest)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy Vault")

	By("waiting for Vault pod to be ready")
	cmd = exec.Command("kubectl", "wait", "--for=condition=ready", "pod/vault",
		"-n", vaultNamespace, "--timeout=120s")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Vault pod not ready")

	By("enabling AppRole auth in Vault")
	cmd = exec.Command("kubectl", "exec", "vault", "-n", vaultNamespace, "--",
		"vault", "auth", "enable", "approle")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to enable AppRole auth")

	By("creating Vault policy for e2e secret access")
	cmd = exec.Command("kubectl", "exec", "vault", "-n", vaultNamespace, "--",
		"sh", "-c",
		`vault policy write e2e-read - <<'HCL'
path "secret/data/e2e/*" { capabilities = ["read"] }
HCL`)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create Vault policy")

	By("creating AppRole role")
	cmd = exec.Command("kubectl", "exec", "vault", "-n", vaultNamespace, "--",
		"vault", "write", "auth/approle/role/e2e-role",
		"token_policies=default,e2e-read", "token_ttl=1h", "token_max_ttl=4h")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create AppRole role")

	By("reading AppRole role-id")
	cmd = exec.Command("kubectl", "exec", "vault", "-n", vaultNamespace, "--",
		"vault", "read", "-format=json", "auth/approle/role/e2e-role/role-id")
	roleIDOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to read role-id")

	var roleIDResp struct {
		Data struct {
			RoleID string `json:"role_id"`
		} `json:"data"`
	}
	jsonStart := strings.Index(roleIDOutput, "{")
	Expect(jsonStart).NotTo(Equal(-1), "No JSON found in role-id output")
	Expect(json.Unmarshal([]byte(roleIDOutput[jsonStart:]), &roleIDResp)).To(Succeed(), "Failed to parse role-id JSON")
	Expect(roleIDResp.Data.RoleID).NotTo(BeEmpty(), "role-id is empty")

	By("generating AppRole secret-id")
	cmd = exec.Command("kubectl", "exec", "vault", "-n", vaultNamespace, "--",
		"vault", "write", "-format=json", "-f", "auth/approle/role/e2e-role/secret-id")
	secretIDOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to generate secret-id")

	var secretIDResp struct {
		Data struct {
			SecretID string `json:"secret_id"`
		} `json:"data"`
	}
	jsonStart = strings.Index(secretIDOutput, "{")
	Expect(jsonStart).NotTo(Equal(-1), "No JSON found in secret-id output")
	Expect(json.Unmarshal([]byte(secretIDOutput[jsonStart:]), &secretIDResp)).To(Succeed(), "Failed to parse secret-id JSON")
	Expect(secretIDResp.Data.SecretID).NotTo(BeEmpty(), "secret-id is empty")

	By("creating fivetran-vault-secret in operator namespace")
	_, _ = fmt.Fprintf(GinkgoWriter, "creating fivetran-vault-secret (credentials redacted from logs)\n")
	vaultAddr := fmt.Sprintf("http://vault.%s.svc.cluster.local:8200", vaultNamespace)
	vaultSecretData := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]string{
			"name":      "fivetran-vault-secret",
			"namespace": namespace,
		},
		"type": "Opaque",
		"stringData": map[string]string{
			"address":   vaultAddr,
			"roleId":    roleIDResp.Data.RoleID,
			"secretId":  secretIDResp.Data.SecretID,
			"mountPath": "secret",
		},
	}
	vaultSecretJSON, jsonErr := json.Marshal(vaultSecretData)
	Expect(jsonErr).NotTo(HaveOccurred(), "Failed to marshal fivetran-vault-secret")
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(vaultSecretJSON)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create fivetran-vault-secret")
}

func teardownVaultDevServer() {
	cmd := exec.Command("kubectl", "delete", "ns", vaultNamespace, "--ignore-not-found=true", "--timeout=60s")
	_, _ = utils.Run(cmd)
}

// applyFivetranConnectorCR writes a CR YAML to a temp file and applies it.
func applyFivetranConnectorCR(name, yamlContent string) {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlContent)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to apply FivetranConnector CR %s", name)
}

// deleteFivetranConnectorCR deletes a CR. Safe to call in cleanup contexts;
// logs warnings instead of failing on errors.
func deleteFivetranConnectorCR(name string) {
	cmd := exec.Command("kubectl", "delete", "fivetranconnector", name,
		"-n", namespace, "--timeout=180s", "--ignore-not-found=true")
	if _, err := utils.Run(cmd); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: failed to delete CR %s: %v\n", name, err)
	}
}

// getConditionStatus returns the status string of a named condition on a CR.
func getConditionStatus(crName, conditionType string) string {
	jsonpath := fmt.Sprintf(`jsonpath={.status.conditions[?(@.type=="%s")].status}`, conditionType)
	cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
		"-n", namespace, "-o", jsonpath)
	output, err := utils.Run(cmd)
	if err != nil {
		return ""
	}
	return output
}

// getConditionMessage returns the message string of a named condition on a CR.
func getConditionMessage(crName, conditionType string) string {
	jsonpath := fmt.Sprintf(
		`jsonpath={.status.conditions[?(@.type=="%s")].message}`, conditionType)
	cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
		"-n", namespace, "-o", jsonpath)
	output, err := utils.Run(cmd)
	if err != nil {
		return ""
	}
	return output
}

// getStatusField returns a field from the CR's .status using a jsonpath expression.
func getStatusField(crName, field string) string {
	cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
		"-n", namespace, "-o", fmt.Sprintf("jsonpath={.status.%s}", field))
	output, err := utils.Run(cmd)
	if err != nil {
		return ""
	}
	return output
}

// crExists returns true if the named FivetranConnector CR still exists.
// Returns (false, nil) only when kubectl confirms "not found".
// Returns (false, error) on transient errors so Gomega retries.
func crExists(crName string) (bool, error) {
	cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
		"-n", namespace, "--no-headers")
	output, err := utils.Run(cmd)
	if err != nil {
		if strings.Contains(output, "not found") || strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// fivetranConnectorExists checks via the Fivetran SDK whether a connector still exists.
// Returns false only when the API confirms the connector is gone (error response).
// Returns true for successful lookups and any unexpected errors
// (fail-safe: assume connector exists if we can't confirm deletion).
func fivetranConnectorExists(connectorID string) bool {
	client := newFivetranSDKClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.Connections.GetConnection(ctx, connectorID)
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter,
			"fivetranConnectorExists(%s): SDK error (treating as not found): %v\n",
			connectorID, err)
		return false
	}
	return true
}

// newFivetranSDKClient creates a Fivetran SDK client for E2E test assertions.
func newFivetranSDKClient() *fivetranPkg.Client {
	client, err := fivetranPkg.NewClient(fivetranAPIKey, fivetranAPISecret)
	Expect(err).NotTo(HaveOccurred(), "Failed to create Fivetran SDK client")
	return client
}

// getConnectorSchemaDetails fetches the schema configuration using the Fivetran SDK.
func getConnectorSchemaDetails(connectorID string) (connections.ConnectionSchemaDetailsResponse, error) {
	client := newFivetranSDKClient()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.Schemas.GetSchemaDetails(ctx, connectorID)
}

// getConnectorDetails fetches the connector details using the Fivetran SDK.
func getConnectorDetails(connectorID string) (connections.DetailsWithCustomConfigNoTestsResponse, error) {
	client := newFivetranSDKClient()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.Connections.GetConnection(ctx, connectorID)
}

// cleanupFivetranConnector deletes a connector via the Fivetran SDK.
// Used as a safety net when the operator's finalizer fails to clean up.
func cleanupFivetranConnector(connectorID string) {
	_, _ = fmt.Fprintf(GinkgoWriter, "Cleaning up connector %s via Fivetran API\n", connectorID)
	client := newFivetranSDKClient()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := client.Connections.DeleteConnection(ctx, connectorID)
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: failed to cleanup connector %s: %v\n", connectorID, err)
	}
}
