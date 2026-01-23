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
	"errors"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
	"github.com/redhat-data-and-ai/fivetran-operator/pkg/fivetran"
	"github.com/redhat-data-and-ai/fivetran-operator/pkg/fivetran/vault"
)

// handleError handles errors by setting appropriate conditions and updating status
func (r *FivetranConnectorReconciler) handleError(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, conditionType, reason string, err error) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Error(err, "Reconcile failed", "conditionType", conditionType, "reason", reason)

	// Check if the error is a schema configuration error (should not requeue)
	if errors.Is(err, ErrSchemaMismatchAfterRetry) {
		return ctrl.Result{}, r.setCondition(ctx, connector, conditionType, metav1.ConditionFalse, reason, err.Error())
	}

	// Check if the error is a setup test error (should not requeue)
	if errors.Is(err, ErrSetupTestsFailed) {
		// Set connector ready condition to true because setup tests failed after connector reconciliation was successful
		if err := r.setCondition(ctx, connector, conditionTypeConnectorReady, metav1.ConditionTrue, ConnectorReasonSuccess, msgConnectorReady); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.setCondition(ctx, connector, conditionType, metav1.ConditionFalse, reason, err.Error())
	}

	// Check if the error is a connector validation error from annotation (should not requeue)
	if errors.Is(err, ErrConnectorValidationFailed) {
		return ctrl.Result{}, r.setCondition(ctx, connector, conditionType, metav1.ConditionFalse, reason, err.Error())
	}

	// Check if the error is a vault resolution error
	var vaultErr *vault.VaultError
	if errors.As(err, &vaultErr) {
		if vaultErr.IsRetryable() {
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, r.setCondition(ctx, connector, conditionType, metav1.ConditionFalse, reason, err.Error())
		}
		return ctrl.Result{}, r.setCondition(ctx, connector, conditionType, metav1.ConditionFalse, reason, err.Error())
	}

	// Check if the error is a Fivetran API error and retryable
	var fivetranErr *fivetran.APIError
	if errors.As(err, &fivetranErr) {
		if fivetranErr.IsRetryable() {
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, r.setCondition(ctx, connector, conditionType, metav1.ConditionFalse, reason, fivetranErr.Error())
		}
		return ctrl.Result{}, r.setCondition(ctx, connector, conditionType, metav1.ConditionFalse, reason, fivetranErr.Error())
	}

	// Set default error condition
	if err := r.setCondition(ctx, connector, conditionType, metav1.ConditionFalse, reason, err.Error()); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, err
}

// updateSetupTestsCondition handles the setup tests condition logic
func (r *FivetranConnectorReconciler) updateSetupTestsCondition(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, setupTestWarnings []string) error {
	// Check if setup tests are enabled (default to true if nil)
	runTests := connector.Spec.Connector.RunSetupTests == nil || *connector.Spec.Connector.RunSetupTests

	if !runTests {
		return r.setCondition(ctx, connector, conditionTypeSetupTestReady, metav1.ConditionTrue, SetupTestsReasonSkipped, msgSetupTestsSkipped)
	}

	// Setup tests were run - determine reason and message based on warnings
	reason := SetupTestsReasonReconciliationSuccess
	message := msgSetupTestsCompletedSuccessfully

	if len(setupTestWarnings) > 0 {
		reason = SetupTestsReasonReconciliationSuccessWithWarnings
		message = fmt.Sprintf(msgSetupTestsWarningsFormat, strings.Join(setupTestWarnings, "; "))
	}

	return r.setCondition(ctx, connector, conditionTypeSetupTestReady, metav1.ConditionTrue, reason, message)
}

// setCondition sets a condition on the connector
func (r *FivetranConnectorReconciler) setCondition(ctx context.Context, connector *operatorv1alpha1.FivetranConnector, conditionType string, status metav1.ConditionStatus, reason, message string) error {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	if connector.Status.Conditions == nil {
		connector.Status.Conditions = []metav1.Condition{}
	}

	for i, existingCondition := range connector.Status.Conditions {
		if existingCondition.Type == condition.Type {
			connector.Status.Conditions[i] = condition
			return r.Status().Update(ctx, connector)
		}
	}

	connector.Status.Conditions = append(connector.Status.Conditions, condition)
	return r.Status().Update(ctx, connector)
}
