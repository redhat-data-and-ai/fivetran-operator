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
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/redhat-data-and-ai/fivetran-operator/test/utils"
)

var _ = Describe("FivetranConnector Lifecycle", Ordered, func() {
	const (
		connectorTimeout  = 10 * time.Minute
		connectorInterval = 5 * time.Second
		deletionTimeout   = 5 * time.Minute
	)

	BeforeAll(func() {
		if !e2eConfigPresent() {
			Skip("Skipping connector lifecycle tests: E2E_SKIP_LIFECYCLE=true")
		}
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			if controllerPodName != "" {
				By("Fetching controller manager pod logs after lifecycle test failure")
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				logs, err := utils.Run(cmd)
				if err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n%s", logs)
				}
			}

			By("Fetching Kubernetes events after lifecycle test failure")
			cmd := exec.Command("kubectl", "get", "events", "-n", namespace,
				"--sort-by=.lastTimestamp")
			events, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", events)
			}
		}
	})

	Context("Google Sheets connector (with deletion)", func() {
		const crName = "e2e-google-sheets"
		var createdConnectorID string

		AfterAll(func() {
			By("ensuring Google Sheets connector CR is cleaned up")
			deleteFivetranConnectorCR(crName)

			By("waiting for CR to be fully removed")
			Eventually(func() (bool, error) {
				return crExists(crName)
			}, deletionTimeout, connectorInterval).Should(BeFalse(),
				"FivetranConnector CR was not deleted in time")

			if createdConnectorID != "" {
				By("verifying connector is removed from Fivetran (safety cleanup)")
				if fivetranConnectorExists(createdConnectorID) {
					_, _ = fmt.Fprintf(GinkgoWriter,
						"WARNING: connector %s still exists in Fivetran after CR deletion, performing direct API cleanup\n",
						createdConnectorID)
					cleanupFivetranConnector(createdConnectorID)
				}
			}
		})

		It("should create the connector and pass setup tests", func() {
			By("applying the Google Sheets FivetranConnector CR")
			applyFivetranConnectorCR(crName, buildGoogleSheetsCR(crName))

			By("waiting for ConnectorReady condition to be True")
			Eventually(func() string {
				return getConditionStatus(crName, "ConnectorReady")
			}, connectorTimeout, connectorInterval).Should(Equal("True"),
				"ConnectorReady did not become True")

			By("waiting for SetupTestReady condition to be True")
			Eventually(func() string {
				return getConditionStatus(crName, "SetupTestReady")
			}, connectorTimeout, connectorInterval).Should(Equal("True"),
				"SetupTestReady did not become True")

			By("waiting for SchemaReady condition to be True")
			Eventually(func() string {
				return getConditionStatus(crName, "SchemaReady")
			}, connectorTimeout, connectorInterval).Should(Equal("True"),
				"SchemaReady did not become True")

			By("verifying connectorId is populated in status")
			createdConnectorID = getStatusField(crName, "connectorId")
			Expect(createdConnectorID).NotTo(BeEmpty(), "status.connectorId should be set")
			_, _ = fmt.Fprintf(GinkgoWriter, "Connector created with ID: %s\n", createdConnectorID)

			By("verifying connectorUrl is populated in status")
			connectorURL := getStatusField(crName, "connectorUrl")
			Expect(connectorURL).To(ContainSubstring("fivetran.com/dashboard/connectors/"),
				"status.connectorUrl should contain the dashboard URL")
		})

		It("should delete the connector from Fivetran when CR is removed", func() {
			By("verifying the CR exists before deletion")
			exists, err := crExists(crName)
			Expect(err).NotTo(HaveOccurred(), "Failed to check CR existence")
			Expect(exists).To(BeTrue(), "CR should exist before deletion test")

			Expect(createdConnectorID).NotTo(BeEmpty(), "connectorId must be set before deletion")
			_, _ = fmt.Fprintf(GinkgoWriter, "Deleting connector with ID: %s\n", createdConnectorID)

			By("deleting the FivetranConnector CR")
			cmd := exec.Command("kubectl", "delete", "fivetranconnector", crName,
				"-n", namespace, "--timeout=180s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete FivetranConnector CR")

			By("waiting for the CR to be fully removed (finalizer completed)")
			Eventually(func() (bool, error) {
				return crExists(crName)
			}, deletionTimeout, connectorInterval).Should(BeFalse(),
				"FivetranConnector CR was not fully deleted — finalizer may have failed")

			By("verifying the connector no longer exists in Fivetran")
			Eventually(func() bool {
				return fivetranConnectorExists(createdConnectorID)
			}, 2*time.Minute, connectorInterval).Should(BeFalse(),
				"Connector should be deleted from Fivetran after CR removal")
		})
	})

	Context("Google Sheets connector (orphan on delete)", func() {
		const crName = "e2e-orphan-test"
		var createdConnectorID string

		AfterAll(func() {
			By("ensuring orphan test CR is cleaned up")
			deleteFivetranConnectorCR(crName)

			By("waiting for CR to be fully removed")
			Eventually(func() (bool, error) {
				return crExists(crName)
			}, deletionTimeout, connectorInterval).Should(BeFalse(),
				"FivetranConnector CR was not deleted in time")

			if createdConnectorID != "" {
				By("cleaning up orphaned connector via Fivetran API")
				cleanupFivetranConnector(createdConnectorID)
			}
		})

		It("should create the connector successfully", func() {
			By("applying the FivetranConnector CR without allow-deletion annotation")
			applyFivetranConnectorCR(crName, buildOrphanCR(crName))

			By("waiting for ConnectorReady condition to be True")
			Eventually(func() string {
				return getConditionStatus(crName, "ConnectorReady")
			}, connectorTimeout, connectorInterval).Should(Equal("True"),
				"ConnectorReady did not become True")

			By("capturing the connectorId for later verification")
			createdConnectorID = getStatusField(crName, "connectorId")
			Expect(createdConnectorID).NotTo(BeEmpty(),
				"status.connectorId should be set")
			_, _ = fmt.Fprintf(GinkgoWriter,
				"Orphan test connector created with ID: %s\n",
				createdConnectorID)
		})

		It("should preserve the Fivetran connector when CR is deleted without allow-deletion", func() {
			Expect(createdConnectorID).NotTo(BeEmpty(),
				"connectorId must be set before orphan test")

			By("deleting the FivetranConnector CR (no allow-deletion annotation)")
			cmd := exec.Command("kubectl", "delete", "fivetranconnector", crName,
				"-n", namespace, "--timeout=180s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(),
				"Failed to delete FivetranConnector CR")

			By("waiting for the CR to be fully removed from Kubernetes")
			Eventually(func() (bool, error) {
				return crExists(crName)
			}, deletionTimeout, connectorInterval).Should(BeFalse(),
				"CR was not removed from Kubernetes")

			By("verifying the connector STILL EXISTS in Fivetran (orphaned)")
			Expect(fivetranConnectorExists(createdConnectorID)).To(BeTrue(),
				"Connector should still exist in Fivetran after "+
					"CR deletion without allow-deletion annotation")

			By("verifying connector details are accessible via API")
			details, err := getConnectorDetails(createdConnectorID)
			Expect(err).NotTo(HaveOccurred(),
				"Should be able to fetch orphaned connector details")
			Expect(details.Data.ID).To(Equal(createdConnectorID),
				"Connector ID should match")

			_, _ = fmt.Fprintf(GinkgoWriter,
				"Confirmed connector %s is preserved (orphaned) in Fivetran\n",
				createdConnectorID)
		})
	})
})
