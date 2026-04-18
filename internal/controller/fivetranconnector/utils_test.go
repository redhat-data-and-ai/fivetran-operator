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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
)

func connectorWithConditions(conditions ...metav1.Condition) *operatorv1alpha1.FivetranConnector {
	return &operatorv1alpha1.FivetranConnector{
		Status: operatorv1alpha1.FivetranConnectorStatus{
			Conditions: conditions,
		},
	}
}

func condition(condType string, status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{
		Type:   condType,
		Status: status,
	}
}

func TestIsConditionFalse_SchemaFailed(t *testing.T) {
	r := &FivetranConnectorReconciler{}
	connector := connectorWithConditions(
		condition("ConnectorReady", metav1.ConditionTrue),
		condition("SetupTestReady", metav1.ConditionTrue),
		condition("SchemaReady", metav1.ConditionFalse),
	)

	if !r.isConditionFalse(connector, "SchemaReady") {
		t.Error("expected SchemaReady to be detected as False")
	}
	if r.isConditionFalse(connector, "ConnectorReady") {
		t.Error("expected ConnectorReady to not be False")
	}
	if r.isConditionFalse(connector, "SetupTestReady") {
		t.Error("expected SetupTestReady to not be False")
	}
}

func TestIsConditionFalse_ConnectorFailed(t *testing.T) {
	r := &FivetranConnectorReconciler{}
	connector := connectorWithConditions(
		condition("ConnectorReady", metav1.ConditionFalse),
	)

	if !r.isConditionFalse(connector, "ConnectorReady") {
		t.Error("expected ConnectorReady to be detected as False")
	}
	if r.isConditionFalse(connector, "SchemaReady") {
		t.Error("expected SchemaReady to not be False when not present")
	}
}

func TestIsConditionFalse_NoConditions(t *testing.T) {
	r := &FivetranConnectorReconciler{}
	connector := &operatorv1alpha1.FivetranConnector{}

	if r.isConditionFalse(connector, "ConnectorReady") {
		t.Error("expected no false conditions on empty connector")
	}
}

func TestIsConditionFalse_AllTrue(t *testing.T) {
	r := &FivetranConnectorReconciler{}
	connector := connectorWithConditions(
		condition("ConnectorReady", metav1.ConditionTrue),
		condition("SetupTestReady", metav1.ConditionTrue),
		condition("SchemaReady", metav1.ConditionTrue),
	)

	if r.isConditionFalse(connector, "ConnectorReady") {
		t.Error("expected ConnectorReady to not be False")
	}
	if r.isConditionFalse(connector, "SetupTestReady") {
		t.Error("expected SetupTestReady to not be False")
	}
	if r.isConditionFalse(connector, "SchemaReady") {
		t.Error("expected SchemaReady to not be False")
	}
}

func TestIsConditionFalse_SetupTestFailed(t *testing.T) {
	r := &FivetranConnectorReconciler{}
	connector := connectorWithConditions(
		condition("ConnectorReady", metav1.ConditionTrue),
		condition("SetupTestReady", metav1.ConditionFalse),
	)

	if !r.isConditionFalse(connector, "SetupTestReady") {
		t.Error("expected SetupTestReady to be detected as False")
	}
	if r.isConditionFalse(connector, "ConnectorReady") {
		t.Error("expected ConnectorReady to not be False")
	}
}

func TestHasFailedConditions_OnlySchemaFailed(t *testing.T) {
	r := &FivetranConnectorReconciler{}
	connector := connectorWithConditions(
		condition("ConnectorReady", metav1.ConditionTrue),
		condition("SetupTestReady", metav1.ConditionTrue),
		condition("SchemaReady", metav1.ConditionFalse),
	)

	if !r.hasFailedConditions(connector) {
		t.Error("expected hasFailedConditions to return true")
	}
}

func TestHasFailedConditions_NoneSet(t *testing.T) {
	r := &FivetranConnectorReconciler{}
	connector := &operatorv1alpha1.FivetranConnector{}

	if r.hasFailedConditions(connector) {
		t.Error("expected hasFailedConditions to return false with no conditions")
	}
}
