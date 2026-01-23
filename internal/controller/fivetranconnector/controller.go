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
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
	"github.com/redhat-data-and-ai/fivetran-operator/internal/kubeutils"
	"github.com/redhat-data-and-ai/fivetran-operator/pkg/fivetran"
	vaultpkg "github.com/redhat-data-and-ai/fivetran-operator/pkg/vault"
)

// FivetranConnectorReconciler reconciles a FivetranConnector object
type FivetranConnectorReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	FivetranClient          *fivetran.Client
	VaultClient             *vaultpkg.VaultClient
	MaxConcurrentReconciles int
}

// +kubebuilder:rbac:groups=operator.dataverse.redhat.com,namespace=fivetran-operator,resources=fivetranconnectors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.dataverse.redhat.com,namespace=fivetran-operator,resources=fivetranconnectors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.dataverse.redhat.com,namespace=fivetran-operator,resources=fivetranconnectors/finalizers,verbs=update
// +kubebuilder:rbac:groups="",namespace=fivetran-operator,resources=secrets,verbs=get;list;watch

func (r *FivetranConnectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Starting reconciliation")

	connector := &operatorv1alpha1.FivetranConnector{}
	if err := r.Get(ctx, req.NamespacedName, connector); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate fivetran client
	if r.FivetranClient == nil {
		return r.handleError(ctx, connector, conditionTypeConnectorReady, ConnectorReasonFivetranClientNotInitialized, ErrFivetranClientNotInitialized)
	}

	// Initialize vault client if it's not present or if the token is not valid
	if r.VaultClient == nil || !vaultpkg.IsTokenValid(r.VaultClient, 300) {
		logger.Info("vault client is not initialized or expired, initializing new client")
		vaultSecretName := os.Getenv(envFivetranVaultSecretName)
		if vaultSecretName == "" {
			vaultSecretName = defaultVaultSecretName
		}
		vaultClient, err := vaultpkg.InitializeVaultClientFromSecret(ctx, r.Client, req.Namespace, vaultSecretName)
		if err != nil {
			err = fmt.Errorf("%w: %w", ErrVaultClientInitializationFailed, err)
			return ctrl.Result{}, err
		}
		r.VaultClient = vaultClient
		logger.Info("vault client initialized successfully")
	}

	// Handle deletion
	if !connector.DeletionTimestamp.IsZero() {
		if err := r.handleDeletion(ctx, connector); err != nil {
			return r.handleError(ctx, connector, conditionTypeConnectorReady, ConnectorReasonDeletionFailed, err)
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer is present
	if err := r.ensureFinalizer(ctx, connector); err != nil {
		return r.handleError(ctx, connector, conditionTypeConnectorReady, ConnectorReasonFinalizerUpdateFailed, err)
	}

	// Check force reconcile flag
	forceReconcile := kubeutils.HasLabel(connector, annotationForceReconcile)

	// Determine what needs to be reconciled
	reconcileConnector, reconcileSchema, err := r.determineReconciliationNeeds(ctx, connector, forceReconcile)
	if err != nil {
		return r.handleError(ctx, connector, conditionTypeConnectorReady, ConnectorReasonReconciliationFailed, err)
	}

	// Early return if nothing to do
	if !reconcileConnector && !reconcileSchema {
		logger.Info("No changes detected and no failures, skipping reconcile")
		return ctrl.Result{}, nil
	}

	// Resolve secrets
	resolvedConfig, resolvedAuth, err := r.resolveSecrets(ctx, connector)
	if err != nil {
		return r.handleError(ctx, connector, conditionTypeConnectorReady, ConnectorReasonVaultSecretsResolutionFailed, err)
	}

	// Get connector ID for operations that need it
	var connectorID string
	if connector.Status.ConnectorID != "" {
		connectorID = connector.Status.ConnectorID
	}

	// Reconcile connector if needed
	var setupTestWarnings []string
	if reconcileConnector {
		connectorID, err = r.reconcileConnector(ctx, connector, resolvedConfig, resolvedAuth)
		if err != nil {
			return r.handleError(ctx, connector, conditionTypeConnectorReady, ConnectorReasonReconciliationFailed, err)
		}

		setupTestWarnings, err = r.reconcileSetupTests(ctx, connector, connectorID)
		if err != nil {
			return r.handleError(ctx, connector, conditionTypeSetupTestReady, SetupTestsReasonReconciliationFailed, err)
		}

		if len(setupTestWarnings) > 0 {
			logger.Info("Setup tests completed with warnings", "warnings", setupTestWarnings)
		}
	}

	// Configure schema if needed
	if reconcileSchema && r.hasSchemaConfig(connector) {
		if err := r.reconcileSchema(ctx, connector, connectorID); err != nil {
			return r.handleError(ctx, connector, conditionTypeSchemaReady, SchemaReasonReconciliationFailed, err)
		}
	} else {
		if err := r.setCondition(ctx, connector, conditionTypeSchemaReady, metav1.ConditionTrue, SchemaReasonSkipped, msgSchemaSkipped); err != nil {
			return r.handleError(ctx, connector, conditionTypeSchemaReady, SchemaReasonSkipped, err)
		}
	}

	// Update connector again to set ScheduleType and pause state
	// This is needed because ScheduleType is not available in createconnector API
	if reconcileConnector {
		logger.Info("Updating connector again to set ScheduleType and pause state")
		if err := r.updateConnector(ctx, connector, connectorID, resolvedConfig, resolvedAuth); err != nil {
			return r.handleError(ctx, connector, conditionTypeConnectorReady, ConnectorReasonReconciliationFailed, err)
		}
	}

	// Clean up annotations and labels
	if err := r.cleanupAnnotationsAndLabels(ctx, connector); err != nil {
		return r.handleError(ctx, connector, conditionTypeConnectorReady, ConnectorReasonReconciliationFailed, err)
	}

	logger.Info("Reconciliation completed")
	return ctrl.Result{}, nil
}

// handleDeletion handles connector deletion
func (r *FivetranConnectorReconciler) handleDeletion(ctx context.Context, connector *operatorv1alpha1.FivetranConnector) error {
	logger := log.FromContext(ctx)
	logger.Info("Handling deletion", "connector", connector.Name, "connectorId", connector.Status.ConnectorID)

	if !controllerutil.ContainsFinalizer(connector, fivetranFinalizer) {
		return nil
	}

	if connector.Status.ConnectorID != "" {
		_, err := r.FivetranClient.Connections.DeleteConnection(ctx, connector.Status.ConnectorID)
		if err != nil {
			return err
		}
		logger.Info("Successfully deleted Fivetran connector", "connectorID", connector.Status.ConnectorID)
	}

	controllerutil.RemoveFinalizer(connector, fivetranFinalizer)

	if err := r.Update(ctx, connector); err != nil {
		logger.Error(err, "failed to remove finalizer")
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FivetranConnectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// add a predicate to the controller to reconcile only when the generation of the CR changes or the force sync label is added
	labelPredicate := kubeutils.CustomLabelKeyChangedPredicate{LabelKey: kubeutils.ForceReconcileLabel}

	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.FivetranConnector{}).
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, labelPredicate)).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Complete(r)
}
