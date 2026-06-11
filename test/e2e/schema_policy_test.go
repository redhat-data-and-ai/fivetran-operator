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

var _ = Describe("Schema Policy Enforcement", Ordered, func() {
	const (
		crName            = "e2e-schema-policy"
		connectorTimeout  = 10 * time.Minute
		connectorInterval = 5 * time.Second
		deletionTimeout   = 5 * time.Minute
	)

	var connectorID string

	BeforeAll(func() {
		if !postgresConfigPresent() {
			Skip("Skipping schema policy tests: " +
				"E2E_POSTGRES_HOST and E2E_POSTGRES_PASSWORD must be set")
		}
	})

	// buildCR creates a FivetranConnector CR YAML with the given connectorSchemas block.
	buildCR := func(schemasBlock string) string {
		cr := fmt.Sprintf(`apiVersion: operator.dataverse.redhat.com/v1alpha1
kind: FivetranConnector
metadata:
  name: %s
  namespace: %s
  annotations:
    operator.dataverse.redhat.com/allow-deletion: "true"
spec:
  connector:
    group_id: "%s"
    service: postgres_rds
    paused: true
    schedule_type: auto
    sync_frequency: 1440
    daily_sync_time: "21:00"
    run_setup_tests: true
    config:
      host: "%s"
      port: 5432
      database: "fivetran_e2e"
      user: "fivetran_e2e"
      password: "vault:e2e/postgres#password"
      schema_prefix: "e2e_schema_policy"
      update_method: "XMIN"
`, crName, namespace, fivetranGroupID,
			postgresHost)

		if schemasBlock != "" {
			cr += "  connectorSchemas:\n" + schemasBlock
		}
		return cr
	}

	// getSchemaAnnotation returns the current schema hash annotation value.
	getSchemaAnnotation := func() string {
		cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
			"-n", namespace, "-o",
			`jsonpath={.metadata.annotations.operator\.dataverse\.redhat\.com/schema-hash}`)
		output, err := utils.Run(cmd)
		if err != nil {
			return ""
		}
		return output
	}

	// applySchemaAndWait updates the CR with a new connectorSchemas block,
	// waits for the operator to detect the change (schema hash annotation changes),
	// then waits for the expected SchemaReady condition.
	applySchemaAndWait := func(testName, schemasBlock, expectedReady string) {
		oldHash := getSchemaAnnotation()

		By("applying schema config: " + testName)
		applyFivetranConnectorCR(crName, buildCR(schemasBlock))

		By("waiting for operator to reconcile schema (hash change)")
		Eventually(func() string {
			return getSchemaAnnotation()
		}, connectorTimeout, connectorInterval).Should(And(Not(BeEmpty()), Not(Equal(oldHash))),
			"Schema hash should change to a new non-empty value after CR update for "+testName)

		By(fmt.Sprintf("waiting for SchemaReady=%s", expectedReady))
		Eventually(func() string {
			return getConditionStatus(crName, "SchemaReady")
		}, connectorTimeout, connectorInterval).Should(Equal(expectedReady),
			fmt.Sprintf("SchemaReady should be %s for: %s", expectedReady, testName))
	}

	// sdkSchemas is the type alias for the SDK schema map.
	type sdkSchemas = map[string]*connections.ConnectionSchemaConfigSchemaResponse

	// getSchemas fetches the schema config from Fivetran via the SDK.
	getSchemas := func() sdkSchemas {
		resp, err := getConnectorSchemaDetails(connectorID)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to get schema details")
		ExpectWithOffset(1, resp.Data.Schemas).NotTo(BeNil(), "schemas should not be nil")
		return resp.Data.Schemas
	}

	// tableEnabled returns whether a table is enabled.
	tableEnabled := func(schemas sdkSchemas, schemaName, tableName string) bool {
		s, ok := schemas[schemaName]
		ExpectWithOffset(1, ok).To(BeTrue(), schemaName+" should exist")
		t, ok := s.Tables[tableName]
		ExpectWithOffset(1, ok).To(BeTrue(), tableName+" should exist in "+schemaName)
		ExpectWithOffset(1, t.Enabled).NotTo(BeNil(), tableName+" enabled should not be nil")
		return *t.Enabled
	}

	// schemaEnabled returns whether a schema is enabled.
	schemaEnabled := func(schemas sdkSchemas, schemaName string) bool {
		s, ok := schemas[schemaName]
		ExpectWithOffset(1, ok).To(BeTrue(), schemaName+" should exist")
		ExpectWithOffset(1, s.Enabled).NotTo(BeNil(), schemaName+" enabled should not be nil")
		return *s.Enabled
	}

	// columnEnabled returns whether a column is enabled.
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

	// --- First test creates the connector WITH schema config ---

	It("BLOCK_ALL — only listed schemas/tables sync", func() {
		By("creating the Postgres connector with BLOCK_ALL schema config")
		applyFivetranConnectorCR(crName, buildCR(`    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`))

		By("waiting for ConnectorReady=True")
		Eventually(func() string {
			return getConditionStatus(crName, "ConnectorReady")
		}, connectorTimeout, connectorInterval).Should(Equal("True"),
			"ConnectorReady did not become True")

		By("waiting for SetupTestReady=True")
		Eventually(func() string {
			return getConditionStatus(crName, "SetupTestReady")
		}, connectorTimeout, connectorInterval).Should(Equal("True"),
			"SetupTestReady did not become True")

		connectorID = getStatusField(crName, "connectorId")
		Expect(connectorID).NotTo(BeEmpty(), "connectorId should be set")
		_, _ = fmt.Fprintf(GinkgoWriter,
			"Schema policy connector created with ID: %s\n", connectorID)

		By("waiting for SchemaReady=True")
		Eventually(func() string {
			return getConditionStatus(crName, "SchemaReady")
		}, connectorTimeout, connectorInterval).Should(Equal("True"),
			"SchemaReady did not become True for BLOCK_ALL")

		By("verifying schema state via Fivetran API")
		schemas := getSchemas()
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
		applySchemaAndWait("ALLOW_COLUMNS tables",
			`    schema_change_handling: ALLOW_COLUMNS
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
`, "True")

		schemas := getSchemas()
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
		applySchemaAndWait("ALLOW_COLUMNS no tables",
			`    schema_change_handling: ALLOW_COLUMNS
    schemas:
      e2e_public:
        enabled: true
`, "True")

		schemas := getSchemas()
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
		applySchemaAndWait("ALLOW_COLUMNS disabled schema",
			`    schema_change_handling: ALLOW_COLUMNS
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
      e2e_inventory:
        enabled: false
`, "True")

		schemas := getSchemas()
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeTrue(),
			"e2e_public should be enabled")
		Expect(schemaEnabled(schemas, "e2e_inventory")).To(BeFalse(),
			"e2e_inventory should be disabled (explicit in CR)")
	})

	// --- Column Scenarios ---

	It("BLOCK_ALL + columns — only listed columns enabled", func() {
		applySchemaAndWait("BLOCK_ALL columns",
			`    schema_change_handling: BLOCK_ALL
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
`, "True")

		schemas := getSchemas()
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
		applySchemaAndWait("ALLOW_COLUMNS disable columns",
			`    schema_change_handling: ALLOW_COLUMNS
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
`, "True")

		schemas := getSchemas()
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
		applySchemaAndWait("BLOCK_ALL no columns block",
			`    schema_change_handling: BLOCK_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`, "True")

		schemas := getSchemas()
		Expect(tableEnabled(schemas, "e2e_public", "users")).To(BeTrue(),
			"users table should be enabled")

		publicSchema := schemas["e2e_public"]
		Expect(publicSchema).NotTo(BeNil())
		usersTable := publicSchema.Tables["users"]
		Expect(usersTable).NotTo(BeNil())
		if len(usersTable.Columns) > 0 {
			_, _ = fmt.Fprintf(GinkgoWriter,
				"Columns present (state preserved from previous test, not reset)\n")
		}
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "id")).To(BeTrue(),
			"id (primary key) should remain enabled regardless")
	})

	It("ALLOW_ALL — only CR items in payload, others untouched", func() {
		applySchemaAndWait("ALLOW_ALL only CR items",
			`    schema_change_handling: ALLOW_ALL
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`, "True")

		schemas := getSchemas()
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeTrue(),
			"e2e_public should be enabled")
		Expect(tableEnabled(schemas, "e2e_public", "users")).To(BeTrue(),
			"users should be enabled")

		if invSchema, ok := schemas["e2e_inventory"]; ok && invSchema != nil {
			_, _ = fmt.Fprintf(GinkgoWriter,
				"e2e_inventory exists with enabled=%v (untouched by ALLOW_ALL)\n",
				invSchema.Enabled)
		}
	})

	It("BLOCK_ALL + hashed column", func() {
		applySchemaAndWait("BLOCK_ALL hashed column",
			`    schema_change_handling: BLOCK_ALL
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
`, "True")

		schemas := getSchemas()
		Expect(columnEnabled(schemas, [2]string{"e2e_public", "users"}, "email")).To(BeTrue(),
			"email should be enabled")

		publicSchema := schemas["e2e_public"]
		Expect(publicSchema).NotTo(BeNil())
		usersTable := publicSchema.Tables["users"]
		Expect(usersTable).NotTo(BeNil())
		emailCol := usersTable.Columns["email"]
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
		applySchemaAndWait("validation_level NONE",
			`    schema_change_handling: BLOCK_ALL
    validation_level: NONE
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`, "True")

		schemas := getSchemas()
		Expect(schemaEnabled(schemas, "e2e_public")).To(BeTrue(),
			"e2e_public should be enabled")
		Expect(tableEnabled(schemas, "e2e_public", "users")).To(BeTrue(),
			"users should be enabled")
	})

	// --- Reconciliation Flow ---

	It("No changes — skip reconcile when hash unchanged", func() {
		hashBefore := getSchemaAnnotation()
		Expect(hashBefore).NotTo(BeEmpty(), "schema hash should exist")

		By("re-applying the same CR (no changes)")
		applyFivetranConnectorCR(crName, buildCR(`    schema_change_handling: BLOCK_ALL
    validation_level: NONE
    schemas:
      e2e_public:
        enabled: true
        tables:
          users:
            enabled: true
            sync_mode: SOFT_DELETE
`))

		By("verifying schema hash remains unchanged")
		Consistently(func() string {
			return getSchemaAnnotation()
		}, 10*time.Second, 1*time.Second).Should(Equal(hashBefore),
			"schema hash should NOT change when CR is unchanged")
	})

	It("Force reconcile via label — triggers reconciliation regardless of hash", func() {
		hashBefore := getSchemaAnnotation()
		Expect(hashBefore).NotTo(BeEmpty(), "schema hash should exist")

		By("applying force-reconcile label")
		cmd := exec.Command("kubectl", "label", "fivetranconnector", crName,
			"operator.dataverse.redhat.com/force-reconcile=true",
			"-n", namespace, "--overwrite")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply force-reconcile label")

		By("waiting for operator to reconcile (SchemaReady stays True)")
		Eventually(func() string {
			return getConditionStatus(crName, "SchemaReady")
		}, connectorTimeout, connectorInterval).Should(Equal("True"),
			"SchemaReady should remain True after force reconcile")

		By("verifying force-reconcile label was removed by operator")
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "fivetranconnector", crName,
				"-n", namespace, "-o",
				`jsonpath={.metadata.labels.operator\.dataverse\.redhat\.com/force-reconcile}`)
			output, _ := utils.Run(cmd)
			return output
		}, 30*time.Second, connectorInterval).Should(BeEmpty(),
			"force-reconcile label should be removed after reconciliation")
	})

	// --- Error Scenario (must be last — sets SchemaReady=False) ---

	It("BLOCK_ALL — locked column in CR should set SchemaReady=False", func() {
		By("applying schema config with locked column disabled")
		applyFivetranConnectorCR(crName, buildCR(`    schema_change_handling: BLOCK_ALL
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

		By("waiting for SchemaReady=False (operator detects locked column)")
		Eventually(func() string {
			return getConditionStatus(crName, "SchemaReady")
		}, connectorTimeout, connectorInterval).Should(Equal("False"),
			"SchemaReady should be False for locked column error")

		condMsg := getConditionMessage(crName, "SchemaReady")
		_, _ = fmt.Fprintf(GinkgoWriter, "SchemaReady message: %s\n", condMsg)
		Expect(condMsg).To(ContainSubstring("locked"),
			"SchemaReady message should mention locked column")
	})

	// --- Cleanup ---

	AfterAll(func() {
		By("cleaning up schema policy connector")
		deleteFivetranConnectorCR(crName)
		Eventually(func() (bool, error) {
			return crExists(crName)
		}, deletionTimeout, connectorInterval).Should(BeFalse(),
			"CR was not deleted in time")

		if connectorID != "" {
			cleanupFivetranConnector(connectorID)
		}
	})
})
