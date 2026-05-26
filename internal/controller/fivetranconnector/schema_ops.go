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

package fivetranconnector

import (
	"context"
	"fmt"

	"github.com/fivetran/go-fivetran/connections"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
	"github.com/redhat-data-and-ai/fivetran-operator/internal/kubeutils"
	"github.com/redhat-data-and-ai/fivetran-operator/pkg/fivetran"
)

// getValidationLevel returns the validation level, defaulting to "TABLES" if not set
func getValidationLevel(connector *operatorv1alpha1.FivetranConnector) string {
	if connector.Spec.ConnectorSchemas == nil || connector.Spec.ConnectorSchemas.ValidationLevel == "" {
		return validationLevelTables
	}
	return connector.Spec.ConnectorSchemas.ValidationLevel
}

// isConnectorSyncing checks if the connector is actively syncing
func (r *FivetranConnectorReconciler) isConnectorSyncing(ctx context.Context, connectorID string) (bool, error) {
	resp, err := r.FivetranClient.Connections.GetConnection(ctx, connectorID)
	if err != nil {
		return false, fmt.Errorf("isConnectorSyncing: %w", err)
	}
	syncState := resp.Data.Status.SyncState
	log.FromContext(ctx).Info("Checked connector sync state", "syncState", syncState)
	return syncState == "syncing", nil
}

// reconcileSchema configures connector schema
func (r *FivetranConnectorReconciler) reconcileSchema(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, connectorID string) error {
	logger := log.FromContext(ctx)
	validationLevel := getValidationLevel(connector)
	logger.Info("Reconciling schema", "validationLevel", validationLevel)

	// Check if connector is actively syncing before attempting schema changes
	syncing, err := r.isConnectorSyncing(ctx, connectorID)
	if err != nil {
		return fmt.Errorf("reconcileSchema: %w", err)
	}
	if syncing {
		return ErrConnectorSyncing
	}

	// Get current schema from Fivetran
	schemaDetails, err := r.FivetranClient.Schemas.GetSchemaDetails(ctx, connectorID)
	if err != nil {
		// Check if schema doesn't exist
		if schemaDetails.Code != SchemaNotFoundError {
			// Other error
			return fmt.Errorf("reconcileSchema: failed to get schema details: %w", err)
		}

		// Schema doesn't exist - handle based on validation level
		if validationLevel == validationLevelNone {
			// Create new schema directly without validation
			if err := r.createNewSchema(ctx, connector, connectorID); err != nil {
				return fmt.Errorf("reconcileSchema: %w", err)
			}
			logger.Info("Schema created successfully without validation", "connectorId", connectorID)
			// Set success condition and return early for NONE validation level
			return r.setCondition(ctx, connector, conditionTypeSchemaReady, metav1.ConditionTrue, SchemaReasonReconciliationSuccess, msgSchemaReady)
		}

		// Reload schema to discover it with validation
		if err := r.reloadSchema(ctx, connectorID); err != nil {
			return fmt.Errorf("reconcileSchema: %w", err)
		}
		logger.Info("Schema discovered after reload", "connectorId", connectorID)

		schemaDetails, err = r.FivetranClient.Schemas.GetSchemaDetails(ctx, connectorID)
		if err != nil {
			return fmt.Errorf("reconcileSchema: failed to get schema details after reload: %w", err)
		}
	} else if validationLevel != validationLevelNone {
		// Schema exists — check if CR references schemas/tables not yet in upstream
		needReload := validateCRAgainstUpstream(connector.Spec.ConnectorSchemas, schemaDetails)
		if needReload {
			logger.Info("CR references schemas/tables not found in upstream, reloading", "connectorId", connectorID)
			if err := r.reloadSchema(ctx, connectorID); err != nil {
				return fmt.Errorf("reconcileSchema: %w", err)
			}
			schemaDetails, err = r.FivetranClient.Schemas.GetSchemaDetails(ctx, connectorID)
			if err != nil {
				return fmt.Errorf("reconcileSchema: failed to get schema details after reload: %w", err)
			}
		}
	}

	// Apply schema configuration
	if err := r.applySchemaWithMerge(ctx, connector, connectorID, schemaDetails); err != nil {
		return fmt.Errorf("reconcileSchema: %w", err)
	}

	// Verify schema was applied correctly
	if validationLevel != validationLevelNone {
		verifyDetails, err := r.FivetranClient.Schemas.GetSchemaDetails(ctx, connectorID)
		if err != nil {
			return fmt.Errorf("reconcileSchema: failed to get schema details after apply: %w", err)
		}

		matches, mismatchDetails := fivetran.CompareSchemaWithCR(verifyDetails, connector.Spec.ConnectorSchemas)
		if !matches {
			return fmt.Errorf("reconcileSchema: %s - %w", mismatchDetails.String(), ErrSchemaMismatch)
		}
	}

	if err := r.setCondition(ctx, connector, conditionTypeSchemaReady, metav1.ConditionTrue, SchemaReasonReconciliationSuccess, msgSchemaReady); err != nil {
		return err
	}
	logger.Info("Schema configuration applied successfully", "connectorId", connectorID)
	return nil
}

// reloadSchema discovers or refreshes the schema from the source.
// Uses exclude_mode=PRESERVE — the merge engine handles enable/disable state.
func (r *FivetranConnectorReconciler) reloadSchema(ctx context.Context, connectorID string) error {
	logger := log.FromContext(ctx)

	const excludeMode = "PRESERVE"
	logger.Info("Reloading schema", "connectorId", connectorID, "excludeMode", excludeMode)
	_, err := r.FivetranClient.Schemas.ReloadSchema(ctx, connectorID, excludeMode)
	if err != nil {
		return fmt.Errorf("reloadSchema: %w", err)
	}

	logger.Info("Schema reloaded successfully", "connectorId", connectorID)
	return nil
}

// validateCRAgainstUpstream returns true if the CR references schemas/tables not yet
// discovered by Fivetran, indicating a reload is needed.
func validateCRAgainstUpstream(crSchema *operatorv1alpha1.ConnectorSchemaConfig, upstream connections.ConnectionSchemaDetailsResponse) bool {
	if crSchema == nil {
		return false
	}
	for schemaName, schemaObj := range crSchema.Schemas {
		if schemaObj == nil || !schemaObj.Enabled {
			continue
		}
		upstreamSchema, exists := upstream.Data.Schemas[schemaName]
		if !exists || upstreamSchema == nil {
			return true
		}
		for tableName, tableObj := range schemaObj.Tables {
			if tableObj == nil || !tableObj.Enabled {
				continue
			}
			if _, tableExists := upstreamSchema.Tables[tableName]; !tableExists {
				return true
			}
		}
	}
	return false
}

// applySchemaWithMerge applies schema configuration using the merge engine
func (r *FivetranConnectorReconciler) applySchemaWithMerge(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, connectorID string, upstream connections.ConnectionSchemaDetailsResponse) error {
	logger := log.FromContext(ctx)
	policy := ""
	if connector.Spec.ConnectorSchemas != nil {
		policy = connector.Spec.ConnectorSchemas.SchemaChangeHandling
	}
	logger.Info("Applying schema configuration with policy merge", "connectorId", connectorID,
		"schemaChangeHandling", policy)

	// Build() is called inside UpdateSchema; build errors surface as "failed to build schema config"
	schema := fivetran.MergeSchemaWithPolicy(upstream, connector.Spec.ConnectorSchemas)

	_, err := r.FivetranClient.Schemas.UpdateSchema(ctx, connectorID, schema)
	if err != nil {
		return fmt.Errorf("applySchemaWithMerge: %w", err)
	}

	return r.updateSchemaHash(ctx, connector)
}

// updateSchemaHash updates only the schema hash annotation
func (r *FivetranConnectorReconciler) updateSchemaHash(ctx context.Context, connector *operatorv1alpha1.FivetranConnector) error {
	hash, err := r.calculateSchemaHash(connector)
	if err != nil {
		return err
	}
	kubeutils.SetAnnotation(connector, annotationSchemaHash, hash)
	return r.Update(ctx, connector)
}

// createNewSchema creates a new schema configuration without reloading the schema from the source.
// This is used when validation_level is NONE to avoid the performance cost of schema reload.
func (r *FivetranConnectorReconciler) createNewSchema(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, connectorID string) error {
	logger := log.FromContext(ctx)
	logger.Info("Creating new schema without validation", "connectorId", connectorID)

	// Uses convertSchema (not MergeSchemaWithPolicy) because there is no upstream state to merge
	// with — the schema doesn't exist yet and validation_level=NONE skips reload/discovery.
	schema := r.convertSchema(connector.Spec.ConnectorSchemas)

	// Use CreateSchema API which creates schema config without requiring schema reload
	_, err := r.FivetranClient.Schemas.CreateSchema(ctx, connectorID, schema)
	if err != nil {
		return fmt.Errorf("createNewSchema: %w", err)
	}

	// Update the schema hash to mark this configuration as applied
	if err := r.updateSchemaHash(ctx, connector); err != nil {
		return fmt.Errorf("createNewSchema: failed to update schema hash: %w", err)
	}

	logger.Info("Schema created successfully", "connectorId", connectorID)
	return nil
}
