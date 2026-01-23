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
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
	"github.com/redhat-data-and-ai/fivetran-operator/internal/kubeutils"
	"github.com/redhat-data-and-ai/fivetran-operator/pkg/fivetran"
	"github.com/redhat-data-and-ai/fivetran-operator/pkg/fivetran/vault"
)

// ensureFinalizer adds the finalizer if it doesn't exist
func (r *FivetranConnectorReconciler) ensureFinalizer(ctx context.Context, connector *operatorv1alpha1.FivetranConnector) error {
	logger := log.FromContext(ctx)
	logger.Info("Ensuring finalizer")
	if !controllerutil.ContainsFinalizer(connector, fivetranFinalizer) {
		logger.Info("Adding finalizer", "finalizer", fivetranFinalizer)
		controllerutil.AddFinalizer(connector, fivetranFinalizer)
		if err := r.Update(ctx, connector); err != nil {
			return err
		}
	}
	return nil
}

// determineReconciliationNeeds determines what components need to be reconciled
func (r *FivetranConnectorReconciler) determineReconciliationNeeds(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, forceReconcile bool) (reconcileConnector, reconcileSchema bool, err error) {
	logger := log.FromContext(ctx)
	logger.Info("Determining reconciliation requirements")
	// Force reconcile or any failed conditions means reconcile everything
	if forceReconcile || r.hasFailedConditions(connector) {
		if forceReconcile {
			logger.Info("Force reconcile requested, reconciling all components")
		} else {
			logger.Info("Previous reconcile failed, retrying all components")
		}
		reconcileConnector = true
		reconcileSchema = r.hasSchemaConfig(connector)
		return reconcileConnector, reconcileSchema, nil
	}

	// Check for connector and schema changes
	connectorHashChanged, err := r.hasConnectorHashChanged(connector)
	if err != nil {
		return false, false, err
	}

	schemaHashChanged, err := r.hasSchemaHashChanged(connector)
	if err != nil {
		return false, false, err
	}

	reconcileConnector = connectorHashChanged
	reconcileSchema = schemaHashChanged
	return reconcileConnector, reconcileSchema, nil
}

// cleanupAnnotationsAndLabels removes force reconcile labels and adoption annotations
func (r *FivetranConnectorReconciler) cleanupAnnotationsAndLabels(ctx context.Context, connector *operatorv1alpha1.FivetranConnector) error {
	logger := log.FromContext(ctx)
	logger.Info("Cleaning up annotations and labels")
	// Remove force reconcile label if it exists
	if kubeutils.HasLabel(connector, annotationForceReconcile) {
		kubeutils.RemoveLabel(connector, annotationForceReconcile)
		if err := r.Update(ctx, connector); err != nil {
			return err
		}
	}

	return nil
}

// resolveSecrets resolves vault secrets in connector config and auth
func (r *FivetranConnectorReconciler) resolveSecrets(ctx context.Context, connector *operatorv1alpha1.FivetranConnector) (*runtime.RawExtension, *runtime.RawExtension, error) {
	logger := log.FromContext(ctx)
	logger.Info("Resolving vault secrets")

	var resolvedConfig, resolvedAuth *runtime.RawExtension
	var allErrors []error

	if connector.Spec.Connector.Config != nil {
		configCopy := connector.Spec.Connector.Config.DeepCopy()
		if err := vault.ResolveSecrets(ctx, r.VaultClient, configCopy); err != nil {
			allErrors = append(allErrors, fmt.Errorf("resolveSecrets: config secrets: %w", err))
		} else {
			resolvedConfig = configCopy
		}
	}

	if connector.Spec.Connector.Auth != nil {
		authCopy := connector.Spec.Connector.Auth.DeepCopy()
		if err := vault.ResolveSecrets(ctx, r.VaultClient, authCopy); err != nil {
			allErrors = append(allErrors, fmt.Errorf("resolveSecrets: auth secrets: %w", err))
		} else {
			resolvedAuth = authCopy
		}
	}

	if len(allErrors) > 0 {
		return nil, nil, errors.Join(allErrors...)
	}

	return resolvedConfig, resolvedAuth, nil
}

// hasSchemaConfig checks if connector has schema configuration
// Returns true if either schemas are provided OR SchemaChangeHandling is set
func (*FivetranConnectorReconciler) hasSchemaConfig(connector *operatorv1alpha1.FivetranConnector) bool {
	if connector.Spec.ConnectorSchemas == nil {
		return false
	}
	// Has schema config if either schemas are provided or SchemaChangeHandling is set
	return len(connector.Spec.ConnectorSchemas.Schemas) > 0 ||
		connector.Spec.ConnectorSchemas.SchemaChangeHandling != ""
}

// hasFailedConditions checks if any conditions are in a failed state
func (*FivetranConnectorReconciler) hasFailedConditions(connector *operatorv1alpha1.FivetranConnector) bool {
	if connector.Status.Conditions == nil {
		return false
	}

	for _, condition := range connector.Status.Conditions {
		if condition.Status == metav1.ConditionFalse {
			return true
		}
	}
	return false
}

// toFivetranConnector converts the K8s connector to Fivetran connector format
func (*FivetranConnectorReconciler) toFivetranConnector(connector *operatorv1alpha1.FivetranConnector, resolvedConfig, resolvedAuth *runtime.RawExtension) (*fivetran.Connector, error) {
	// Convert RawExtension to map[string]any for config
	var config map[string]any
	if resolvedConfig != nil && len(resolvedConfig.Raw) > 0 {
		if err := json.Unmarshal(resolvedConfig.Raw, &config); err != nil {
			return nil, fmt.Errorf("toFivetranConnector: failed to unmarshal config: %w", err)
		}
	}

	// Convert RawExtension to map[string]any for auth
	var auth map[string]any
	if resolvedAuth != nil && len(resolvedAuth.Raw) > 0 {
		if err := json.Unmarshal(resolvedAuth.Raw, &auth); err != nil {
			return nil, fmt.Errorf("toFivetranConnector: failed to unmarshal auth: %w", err)
		}
	}

	fivetranConnector := &fivetran.Connector{
		Service:                 connector.Spec.Connector.Service,
		Config:                  &config,
		Auth:                    &auth,
		Paused:                  connector.Spec.Connector.Paused,
		GroupID:                 connector.Spec.Connector.GroupID,
		SyncFrequency:           connector.Spec.Connector.SyncFrequency,
		DailySyncTime:           connector.Spec.Connector.DailySyncTime,
		RunSetupTests:           connector.Spec.Connector.RunSetupTests,
		ScheduleType:            connector.Spec.Connector.ScheduleType,
		PauseAfterTrial:         connector.Spec.Connector.PauseAfterTrial,
		TrustCertificates:       connector.Spec.Connector.TrustCertificates,
		TrustFingerprints:       connector.Spec.Connector.TrustFingerprints,
		DataDelaySensitivity:    connector.Spec.Connector.DataDelaySensitivity,
		DataDelayThreshold:      connector.Spec.Connector.DataDelayThreshold,
		NetworkingMethod:        connector.Spec.Connector.NetworkingMethod,
		ProxyAgentID:            connector.Spec.Connector.ProxyAgentID,
		PrivateLinkID:           connector.Spec.Connector.PrivateLinkID,
		HybridDeploymentAgentID: connector.Spec.Connector.HybridDeploymentAgentID,
	}

	return fivetranConnector, nil
}

// convertSchema converts the API schema to Fivetran schema format
func (r *FivetranConnectorReconciler) convertSchema(apiSchema *operatorv1alpha1.ConnectorSchemaConfig) *fivetran.SchemaBuilder {
	if apiSchema == nil {
		return nil
	}

	// Create builder - schemas and SchemaChangeHandling are now optional
	builder := fivetran.NewSchemaBuilder()

	// Set schema change handling if provided
	if apiSchema.SchemaChangeHandling != "" {
		builder = builder.WithSchemaChangeHandling(apiSchema.SchemaChangeHandling)
	}

	// Add schemas if provided
	if len(apiSchema.Schemas) > 0 {
		for schemaName, schema := range apiSchema.Schemas {
			if schema == nil {
				continue
			}

			builder.AddSchema(schemaName, schema.Enabled)
			r.processSchemaTable(builder, schemaName, schema.Tables)
		}
	}

	return builder
}

// processSchemaTable processes schema table configuration
func (r *FivetranConnectorReconciler) processSchemaTable(builder *fivetran.SchemaBuilder, schemaName string, tables map[string]*operatorv1alpha1.TableObject) {
	for tableName, table := range tables {
		if table == nil {
			continue
		}

		builder.AddTable(schemaName, tableName, table.Enabled, table.SyncMode)
		r.processTableColumns(builder, schemaName, tableName, table.Columns)
	}
}

// processTableColumns processes table column configuration
func (*FivetranConnectorReconciler) processTableColumns(builder *fivetran.SchemaBuilder, schemaName, tableName string, columns map[string]*operatorv1alpha1.ColumnObject) {
	for columnName, column := range columns {
		if column == nil {
			continue
		}

		builder.AddColumn(schemaName, tableName, columnName,
			column.Enabled,
			column.Hashed,
			column.IsPrimaryKey)
	}
}

// Hash calculation functions

// calculateConnectorHash calculates a hash of the connector configuration
func (*FivetranConnectorReconciler) calculateConnectorHash(connector *operatorv1alpha1.FivetranConnector) (string, error) {
	bytes, err := json.Marshal(connector.Spec.Connector)
	if err != nil {
		return "", err
	}

	hash := md5.Sum(bytes)
	return fmt.Sprintf("%x", hash), nil
}

// calculateSchemaHash calculates a hash of the schema configuration
func (*FivetranConnectorReconciler) calculateSchemaHash(connector *operatorv1alpha1.FivetranConnector) (string, error) {
	if connector.Spec.ConnectorSchemas == nil {
		return "", nil
	}

	bytes, err := json.Marshal(connector.Spec.ConnectorSchemas)
	if err != nil {
		return "", err
	}

	hash := md5.Sum(bytes)
	return fmt.Sprintf("%x", hash), nil
}

// hasConnectorHashChanged checks if the connector configuration has changed by comparing hashes
func (r *FivetranConnectorReconciler) hasConnectorHashChanged(connector *operatorv1alpha1.FivetranConnector) (bool, error) {
	currentConnectorHash, err := r.calculateConnectorHash(connector)
	if err != nil {
		return false, fmt.Errorf("hasConnectorHashChanged: %w", err)
	}

	storedConnectorHash := kubeutils.GetAnnotation(connector, annotationConnectorHash)
	return currentConnectorHash != storedConnectorHash, nil
}

// hasSchemaHashChanged checks if the schema configuration needs to be applied
func (r *FivetranConnectorReconciler) hasSchemaHashChanged(connector *operatorv1alpha1.FivetranConnector) (bool, error) {
	// If no schema config is present, it hasn't changed
	if !r.hasSchemaConfig(connector) {
		return false, nil
	}

	if connector.Status.ConnectorID == "" {
		return true, nil
	}

	currentSchemaHash, err := r.calculateSchemaHash(connector)
	if err != nil {
		return false, fmt.Errorf("hasSchemaHashChanged: %w", err)
	}

	storedSchemaHash := kubeutils.GetAnnotation(connector, annotationSchemaHash)
	return currentSchemaHash != storedSchemaHash, nil
}
