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
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
	"github.com/redhat-data-and-ai/fivetran-operator/internal/kubeutils"
)

// reconcileConnector creates or updates connector as needed
func (r *FivetranConnectorReconciler) reconcileConnector(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, resolvedConfig, resolvedAuth *runtime.RawExtension) (string, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling connector")

	var connectorID string
	var createdNewConnector bool

	if connectorID = connector.Status.ConnectorID; connectorID != "" {
		logger.Info("Using existing connector ID from status", "connectorID", connectorID)
		if err := r.updateConnector(ctx, connector, connectorID, resolvedConfig, resolvedAuth); err != nil {
			return "", err
		}
	} else if connectorIDFromAnnotation := kubeutils.GetAnnotation(connector, annotationConnectorID); connectorIDFromAnnotation != "" {
		// Recovery/adoption from annotation
		logger.Info("Found connector ID in annotation, attempting recovery/adoption", "connectorIDFromAnnotation", connectorIDFromAnnotation)
		if err := r.handleConnectorFromAnnotation(ctx, connector, connectorIDFromAnnotation); err != nil {
			logger.Error(err, "Failed to recover/adopt connector from annotation", "connectorIDFromAnnotation", connectorIDFromAnnotation)
			return "", fmt.Errorf("%w: ID %s, details: %v", ErrConnectorValidationFailed, connectorIDFromAnnotation, err)
		}
		connectorID = connectorIDFromAnnotation
		if err := r.updateConnector(ctx, connector, connectorID, resolvedConfig, resolvedAuth); err != nil {
			return "", err
		}
	} else {
		// Create new connector
		logger.Info("No existing connector found, creating new one")
		var err error
		connectorID, err = r.createConnector(ctx, connector, resolvedConfig, resolvedAuth)
		if err != nil {
			return "", err
		}
		if err := r.setConnectorID(ctx, connector, connectorID); err != nil {
			return "", err
		}
		createdNewConnector = true
	}

	if err := r.updateConnectorHash(ctx, connector); err != nil {
		return "", err
	}

	if err := r.setCondition(ctx, connector, conditionTypeConnectorReady, metav1.ConditionTrue, ConnectorReasonSuccess, msgConnectorReady); err != nil {
		return "", err
	}

	if createdNewConnector {
		logger.Info("Connector created successfully", "connectorId", connectorID)
	} else {
		logger.Info("Connector updated successfully", "connectorId", connectorID)
	}

	return connectorID, nil
}

// createConnector creates a new Fivetran connector
func (r *FivetranConnectorReconciler) createConnector(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, resolvedConfig, resolvedAuth *runtime.RawExtension) (string, error) {
	logger := log.FromContext(ctx)
	logger.Info("Creating new Fivetran connector")
	fivetranConnector, err := r.toFivetranConnector(connector, resolvedConfig, resolvedAuth)
	if err != nil {
		return "", err
	}

	// Always create paused during creation
	pausedTrue := true
	fivetranConnector.Paused = &pausedTrue

	resp, err := r.FivetranClient.Connections.CreateConnection(ctx, fivetranConnector)
	if err != nil {
		return "", err
	}

	return resp.Data.ID, nil
}

// updateConnector updates connector
func (r *FivetranConnectorReconciler) updateConnector(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, connectorID string, resolvedConfig, resolvedAuth *runtime.RawExtension) error {
	logger := log.FromContext(ctx)
	logger.Info("Updating Fivetran connector")
	fivetranConnector, err := r.toFivetranConnector(connector, resolvedConfig, resolvedAuth)
	if err != nil {
		return err
	}
	_, err = r.FivetranClient.Connections.UpdateConnection(ctx, connectorID, fivetranConnector)
	return err
}

// handleConnectorFromAnnotation validates and recovers/adopts connector ID from annotation
// This function handles both automatic recovery (when operator lost status) and manual adoption (when user sets annotation)
func (r *FivetranConnectorReconciler) handleConnectorFromAnnotation(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, annotationConnectorID string) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting connector recovery/adoption from annotation", "annotationConnectorID", annotationConnectorID)

	// Validate the connector exists and get its details
	existingConnector, err := r.FivetranClient.Connections.GetConnection(ctx, annotationConnectorID)
	if err != nil {
		return fmt.Errorf("handleConnectorFromAnnotation: failed to get existing connector %s: %w", annotationConnectorID, err)
	}

	// Validate service type matches
	if connector.Spec.Connector.Service != existingConnector.Data.Service {
		return fmt.Errorf("handleConnectorFromAnnotation: service mismatch: spec has '%s', existing connector has '%s'",
			connector.Spec.Connector.Service, existingConnector.Data.Service)
	}

	// Validate group ID matches
	if connector.Spec.Connector.GroupID != existingConnector.Data.GroupID {
		return fmt.Errorf("handleConnectorFromAnnotation: group_id mismatch: spec has '%s', existing connector has '%s'",
			connector.Spec.Connector.GroupID, existingConnector.Data.GroupID)
	}

	// Validate schema configuration when recovering/adopting from annotation
	// This ensures the Kubernetes resource configuration matches the actual Fivetran connector schema
	if connector.Spec.Connector.Config != nil {
		var connectorConfig map[string]any
		if err := json.Unmarshal(connector.Spec.Connector.Config.Raw, &connectorConfig); err == nil {
			var expectedSchema string

			// Determine the expected schema name based on connector configuration
			// Priority order: schema_prefix > schema (with optional table/table_group_name suffix)
			if prefix, ok := connectorConfig["schema_prefix"].(string); ok && prefix != "" {
				// Use schema_prefix if explicitly set (highest priority)
				expectedSchema = prefix
			} else if schema, ok := connectorConfig["schema"].(string); ok && schema != "" {
				// Use base schema name
				expectedSchema = schema

				// For single-table connectors, append table name to schema
				if table, ok := connectorConfig["table"].(string); ok && table != "" {
					expectedSchema = schema + "." + table
				}

				// For table group connectors, append table group name to schema
				if tablegroupname, ok := connectorConfig["table_group_name"]; ok && tablegroupname != "" {
					expectedSchema = schema + "." + tablegroupname.(string)
				}
			}

			// Verify the expected schema matches the existing connector's actual schema
			if expectedSchema != "" && expectedSchema != existingConnector.Data.Schema {
				return fmt.Errorf("handleConnectorFromAnnotation: schema mismatch: expected '%s', got '%s'", expectedSchema, existingConnector.Data.Schema)
			}
		} else {
			return fmt.Errorf("handleConnectorFromAnnotation: failed to unmarshal config: %w", err)
		}
	}

	// Set the connector ID in status (only after all validations pass)
	if err := r.setConnectorID(ctx, connector, annotationConnectorID); err != nil {
		return err
	}

	logger.Info("Successfully recovered/adopted connector from annotation", "connectorID", annotationConnectorID,
		"service", existingConnector.Data.Service, "groupID", existingConnector.Data.GroupID, "schema", existingConnector.Data.Schema)
	return nil
}

// setConnectorID sets the connector ID in status, URL, and annotation backup
func (r *FivetranConnectorReconciler) setConnectorID(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, connectorID string) error {
	logger := log.FromContext(ctx)
	logger.Info("Setting connector ID in status and annotation", "connectorID", connectorID)

	// Update status
	connector.Status.ConnectorID = connectorID
	connector.Status.ConnectorURL = fmt.Sprintf(fivetranConnectorURL, connectorID)

	// Update both status and annotations
	if err := r.Status().Update(ctx, connector); err != nil {
		return err
	}
	// Also store in annotation as backup
	kubeutils.SetAnnotation(connector, annotationConnectorID, connectorID)

	return r.Update(ctx, connector)
}

// updateConnectorHash updates only the connector hash annotation
func (r *FivetranConnectorReconciler) updateConnectorHash(ctx context.Context, connector *operatorv1alpha1.FivetranConnector) error {
	logger := log.FromContext(ctx)
	logger.Info("Updating connector hash")
	hash, err := r.calculateConnectorHash(connector)
	if err != nil {
		return err
	}
	kubeutils.SetAnnotation(connector, annotationConnectorHash, hash)
	return r.Update(ctx, connector)
}
