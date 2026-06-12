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

	"github.com/fivetran/go-fivetran/connections"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/redhat-data-and-ai/fivetran-operator/test/utils"
)

var _ = Describe("Schema Policy Enforcement", func() {
	const (
		connectorTimeout  = 10 * time.Minute
		connectorInterval = 5 * time.Second
	)

	BeforeEach(func() {
		if !postgresConfigPresent() {
			Skip("Skipping schema policy tests: " +
				"E2E_POSTGRES_HOST and E2E_POSTGRES_PASSWORD must be set")
		}
	})

	type sdkSchemas = map[string]*connections.ConnectionSchemaConfigSchemaResponse

	getSchemas := func(connectorID string) sdkSchemas {
		resp, err := getConnectorSchemaDetails(connectorID)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to get schema details")
		ExpectWithOffset(1, resp.Data.Schemas).NotTo(BeNil(), "schemas should not be nil")
		return resp.Data.Schemas
	}

	tableEnabled := func(schemas sdkSchemas, schemaName, tableName string) bool {
		s, ok := schemas[schemaName]
		ExpectWithOffset(1, ok).To(BeTrue(), schemaName+" should exist")
		t, ok := s.Tables[tableName]
		ExpectWithOffset(1, ok).To(BeTrue(), tableName+" should exist in "+schemaName)
		ExpectWithOffset(1, t.Enabled).NotTo(BeNil(), tableName+" enabled should not be nil")
		return *t.Enabled
	}

	schemaEnabled := func(schemas sdkSchemas, schemaName string) bool {
		s, ok := schemas[schemaName]
		ExpectWithOffset(1, ok).To(BeTrue(), schemaName+" should exist")
		ExpectWithOffset(1, s.Enabled).NotTo(BeNil(), schemaName+" enabled should not be nil")
		return *s.Enabled
	}

	columnEnabled := func(schemas sdkSchemas, tablePath [2]string, colName string) bool {
		s, ok := schemas[tablePath[0]]
		ExpectWithOffset(1, ok).To(BeTrue(), tablePath[0]+" should exist")
		t, ok := s.Tables[tablePath[1]]
		ExpectWithOffset(1, ok).To(BeTrue(),
			tablePath[1]+" should exist in "+tablePath[0])
		ExpectWithOffset(1, t.Columns).NotTo(BeNil(),
			tablePath[1]+" should have columns")
		c, ok := t.Columns[colName]
		ExpectWithOffset(1, ok).To(BeTrue(),
			colName+" should exist in "+tablePath[1])
		ExpectWithOffset(1, c.Enabled).NotTo(BeNil(),
			colName+" enabled should not be nil")
		return *c.Enabled
	}

	// createConnector creates a Postgres connector with the given schema config,
	// waits for ConnectorReady, SetupTestReady, and SchemaReady to become True,
	// and returns the Fivetran connector ID.
	createConnector := func(crName, schemasBlock string) string {
		applyFivetranConnectorCR(crName, buildPostgresCR(crName, schemasBlock))

		Eventually(func() string {
			return getConditionStatus(crName, "ConnectorReady")
		}, connectorTimeout, connectorInterval).Should(Equal("True"),
			"ConnectorReady did not become True")

		Eventually(func() string {
			return getConditionStatus(crName, "SetupTestReady")
		}, connectorTimeout, connectorInterval).Should(Equal("True"),
			"SetupTestReady did not become True")

		connectorID := getStatusField(crName, "connectorId")
		Expect(connectorID).NotTo(BeEmpty(), "connectorId should be set")
		_, _ = fmt.Fprintf(GinkgoWriter, "Connector %s created with ID: %s\n", crName, connectorID)

		Eventually(func() string {
			return getConditionStatus(crName, "SchemaReady")
		}, connectorTimeout, connectorInterval).Should(Equal("True"),
			"SchemaReady did not become True")

		return connectorID
	}

	// updateSchema applies a new schema config to an existing connector and waits
	// for the operator to reconcile (schema hash changes + SchemaReady matches expected).
	updateSchema := func(crName, schemasBlock, expectedReady string) {
		oldHash := getSchemaAnnotation(crName)

		applyFivetranConnectorCR(crName, buildPostgresCR(crName, schemasBlock))

		Eventually(func() string {
			return getSchemaAnnotation(crName)
		}, connectorTimeout, connectorInterval).Should(
			And(Not(BeEmpty()), Not(Equal(oldHash))),
			"schema hash should change after CR update")

		Eventually(func() string {
			return getConditionStatus(crName, "SchemaReady")
		}, connectorTimeout, connectorInterval).Should(Equal(expectedReady),
			fmt.Sprintf("SchemaReady should be %s", expectedReady))
	}

	cleanup := func(crName, connectorID string) {
		deleteFivetranConnectorCR(crName)
		if connectorID != "" {
			cleanupFivetranConnector(connectorID)
		}
	}

	// --- Schema/Table Policy Tests ---

	It("BLOCK_ALL — only listed schemas/tables sync", func() {
		crName := "e2e-sp-01"
		connectorID := createConnector(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		schemas := getSchemas(connectorID)
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeTrue(),
			"e2e_public should be enabled")
		Expect(tableEnabled(schemas, "e2e_public", "users")).To(BeTrue(),
			"users should be enabled")
		Expect(tableEnabled(schemas, "e2e_public", "orders")).To(BeFalse(),
			"orders should be disabled (not in CR)")
		Expect(tableEnabled(schemas, "e2e_public", "logs")).To(BeFalse(),
			"logs should be disabled (not in CR)")
		Expect(schemaEnabled(schemas, "e2e_inventory")).To(BeFalse(),
			"e2e_inventory should be disabled (not in CR)")
		Expect(schemaEnabled(schemas, "e2e_analytics")).To(BeFalse(),
			"e2e_analytics should be disabled (not in CR)")
	})

	It("ALLOW_COLUMNS — listed tables enabled, unlisted disabled", func() {
		crName := "e2e-sp-02"
		connectorID := createConnector(crName, `    schema_change_handling: ALLOW_COLUMNS
    schemas:
      e2e_inventory:
        enabled: true
        tables:
          products:
            enabled: true
            sync_mode: SOFT_DELETE
      e2e_analytics:
        enabled: true
        tables:
          page_views:
            enabled: true
            sync_mode: SOFT_DELETE
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		schemas := getSchemas(connectorID)
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeFalse(),
			"e2e_public should be disabled (not in CR)")
		Expect(schemaEnabled(schemas, "e2e_inventory")).To(BeTrue(),
			"e2e_inventory should be enabled")
		Expect(tableEnabled(schemas, "e2e_inventory", "products")).To(BeTrue(),
			"products should be enabled")
		Expect(tableEnabled(schemas, "e2e_inventory", "warehouses")).To(BeFalse(),
			"warehouses should be disabled (not in CR)")
		Expect(schemaEnabled(schemas, "e2e_analytics")).To(BeTrue(),
			"e2e_analytics should be enabled")
		Expect(tableEnabled(schemas, "e2e_analytics", "page_views")).To(BeTrue(),
			"page_views should be enabled")
		Expect(tableEnabled(schemas, "e2e_analytics", "sessions")).To(BeFalse(),
			"sessions should be disabled (not in CR)")
	})

	It("ALLOW_COLUMNS — enabled schema with NO tables listed", func() {
		crName := "e2e-sp-03"
		connectorID := createConnector(crName, `    schema_change_handling: ALLOW_COLUMNS
    schemas:
      e2e_public:
        enabled: true
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		schemas := getSchemas(connectorID)
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeTrue(),
			"e2e_public should be enabled")

		publicSchema := schemas["e2e_public"]
		Expect(publicSchema).NotTo(BeNil())
		for tableName, table := range publicSchema.Tables {
			Expect(table.Enabled).NotTo(BeNil())
			Expect(*table.Enabled).To(BeFalse(),
				fmt.Sprintf("%s should be disabled (no tables in CR)", tableName))
		}
	})

	It("ALLOW_COLUMNS — disabled schema stays disabled", func() {
		crName := "e2e-sp-04"
		connectorID := createConnector(crName, `    schema_change_handling: ALLOW_COLUMNS
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
      e2e_inventory:
        enabled: false
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		schemas := getSchemas(connectorID)
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeTrue(),
			"e2e_public should be enabled")
		Expect(schemaEnabled(schemas, "e2e_inventory")).To(BeFalse(),
			"e2e_inventory should be disabled (explicit in CR)")
	})

	// --- Column Scenarios ---

	It("BLOCK_ALL + columns — only listed columns enabled", func() {
		crName := "e2e-sp-05"
		connectorID := createConnector(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
            columns:
              name:
                enabled: true
              email:
                enabled: true
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		schemas := getSchemas(connectorID)
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "name")).To(BeTrue(),
			"name should be enabled (in CR)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "email")).To(BeTrue(),
			"email should be enabled (in CR)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "password")).To(BeFalse(),
			"password should be disabled (not in CR, BLOCK_ALL)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "created_at")).To(BeFalse(),
			"created_at should be disabled (not in CR, BLOCK_ALL)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "id")).To(BeTrue(),
			"id should remain enabled (locked primary key)")
	})

	It("ALLOW_COLUMNS + columns — disable specific columns", func() {
		crName := "e2e-sp-06"
		connectorID := createConnector(crName, `    schema_change_handling: ALLOW_COLUMNS
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
            columns:
              password:
                enabled: false
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		schemas := getSchemas(connectorID)
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "password")).To(BeFalse(),
			"password should be disabled (explicit in CR)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "name")).To(BeTrue(),
			"name should be enabled (ALLOW_COLUMNS default)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "email")).To(BeTrue(),
			"email should be enabled (ALLOW_COLUMNS default)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "id")).To(BeTrue(),
			"id should remain enabled (locked primary key)")
	})

	It("BLOCK_ALL — no columns block means columns NOT managed", func() {
		crName := "e2e-sp-07"
		// Step 1: Create with columns managed (password disabled via BLOCK_ALL)
		connectorID := createConnector(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
            columns:
              name:
                enabled: true
              email:
                enabled: true
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		// Step 2: Update without columns block — columns should be untouched
		updateSchema(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`, "True")

		schemas := getSchemas(connectorID)
		Expect(tableEnabled(schemas, "e2e_public", "users")).To(BeTrue(),
			"users table should be enabled")
		Expect(schemas["e2e_public"].Tables["users"].Columns).ToNot(BeEmpty(),
			"columns should be present (state preserved)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "id")).To(BeTrue(),
			"id (primary key) should remain enabled regardless")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "password")).To(BeFalse(),
			"password should still be disabled (columns not managed, state preserved)")
	})

	It("ALLOW_ALL — only CR items in payload, others untouched", func() {
		crName := "e2e-sp-08"
		// Step 1: Create with BLOCK_ALL to disable e2e_inventory
		connectorID := createConnector(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		// Step 2: Switch to ALLOW_ALL — non-CR schemas should be untouched
		updateSchema(crName, `    schema_change_handling: ALLOW_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`, "True")

		schemas := getSchemas(connectorID)
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeTrue(),
			"e2e_public should be enabled")
		Expect(tableEnabled(schemas, "e2e_public", "users")).To(BeTrue(),
			"users should be enabled")
		Expect(schemaEnabled(schemas, "e2e_inventory")).To(BeFalse(),
			"e2e_inventory should be untouched (still disabled from BLOCK_ALL)")
	})

	It("BLOCK_ALL + hashed column", func() {
		crName := "e2e-sp-09"
		connectorID := createConnector(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
            columns:
              name:
                enabled: true
              email:
                enabled: true
                hashed: true
              password:
                enabled: false
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		schemas := getSchemas(connectorID)
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "email")).To(BeTrue(),
			"email should be enabled")

		emailCol := schemas["e2e_public"].Tables["users"].Columns["email"]
		Expect(emailCol).NotTo(BeNil())
		Expect(emailCol.Hashed).NotTo(BeNil())
		Expect(*emailCol.Hashed).To(BeTrue(),
			"email should be hashed")

		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "password")).To(BeFalse(),
			"password should be disabled (explicit in CR)")
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "name")).To(BeTrue(),
			"name should be enabled")
	})

	It("validation_level NONE — schema applied without verification", func() {
		crName := "e2e-sp-10"
		connectorID := createConnector(crName, `    schema_change_handling: BLOCK_ALL
    validation_level: NONE
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		schemas := getSchemas(connectorID)
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeTrue(),
			"e2e_public should be enabled")
		Expect(tableEnabled(schemas, "e2e_public", "users")).To(BeTrue(),
			"users should be enabled")
	})

	// --- Reconciliation Flow ---

	It("No changes — skip reconcile when hash unchanged", func() {
		crName := "e2e-sp-11"
		schemaBlock := `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`
		connectorID := createConnector(crName, schemaBlock)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		hashBefore := getSchemaAnnotation(crName)
		Expect(hashBefore).NotTo(BeEmpty(), "schema hash should exist")

		By("re-applying the same CR (no changes)")
		applyFivetranConnectorCR(crName, buildPostgresCR(crName, schemaBlock))

		By("verifying schema hash remains unchanged")
		Consistently(func() string {
			return getSchemaAnnotation(crName)
		}, 10*time.Second, 1*time.Second).Should(Equal(hashBefore),
			"schema hash should NOT change when CR is unchanged")
	})

	It("Force reconcile via label — triggers reconciliation regardless of hash", func() {
		crName := "e2e-sp-12"
		connectorID := createConnector(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		By("capturing resourceVersion before force reconcile")
		cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
			"-n", namespace, "-o", "jsonpath={.metadata.resourceVersion}")
		rvBefore, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(rvBefore).NotTo(BeEmpty())

		By("applying force-reconcile label")
		cmd = exec.Command("kubectl", "label", "fivetranconnector", crName,
			"operator.dataverse.redhat.com/force-reconcile=true",
			"-n", namespace, "--overwrite")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply force-reconcile label")

		By("verifying force-reconcile label was removed by operator")
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
				"-n", namespace, "-o",
				`jsonpath={.metadata.labels.operator\.dataverse\.redhat\.com/force-reconcile}`)
			output, _ := utils.Run(cmd)
			return output
		}, 30*time.Second, connectorInterval).Should(BeEmpty(),
			"force-reconcile label should be removed after reconciliation")

		By("verifying operator reconciled (resourceVersion changed)")
		cmd = exec.Command("kubectl", "get", "fivetranconnector", crName,
			"-n", namespace, "-o", "jsonpath={.metadata.resourceVersion}")
		rvAfter, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(rvAfter).NotTo(Equal(rvBefore),
			"resourceVersion should change after force reconcile, proving operator ran")

		By("verifying SchemaReady is still True")
		Expect(getConditionStatus(crName, "SchemaReady")).To(Equal("True"),
			"SchemaReady should remain True after force reconcile")
	})

	// --- Error Scenario ---

	It("BLOCK_ALL — locked column in CR should set SchemaReady=False", func() {
		crName := "e2e-sp-13"
		// Create with valid config first so the operator learns about columns
		connectorID := createConnector(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`)
		DeferCleanup(func() { cleanup(crName, connectorID) })

		// Apply config that disables a locked column — don't wait for hash change
		// since the operator rejects the schema and never updates the hash.
		applyFivetranConnectorCR(crName, buildPostgresCR(crName, `    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
            columns:
              id:
                enabled: false
              name:
                enabled: true
`))

		Eventually(func() string {
			return getConditionStatus(crName, "SchemaReady")
		}, connectorTimeout, connectorInterval).Should(Equal("False"),
			"SchemaReady should be False for locked column error")

		condMsg := getConditionMessage(crName, "SchemaReady")
		_, _ = fmt.Fprintf(GinkgoWriter, "SchemaReady message: %s\n", condMsg)
		Expect(condMsg).To(ContainSubstring("locked"),
			"SchemaReady message should mention locked column")
	})
})
