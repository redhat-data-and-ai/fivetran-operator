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

import "errors"

const (
	// Controller constants
	fivetranFinalizer    = "fivetran.dataverse.redhat.com/finalizer"
	fivetranConnectorURL = "https://fivetran.com/dashboard/connectors/%s"

	// Annotation constants
	annotationForceReconcile = "operator.dataverse.redhat.com/force-reconcile"
	annotationConnectorHash  = "operator.dataverse.redhat.com/connector-hash"
	annotationSchemaHash     = "operator.dataverse.redhat.com/schema-hash"
	annotationConnectorID    = "operator.dataverse.redhat.com/connector-id"

	// Condition types
	conditionTypeConnectorReady = "ConnectorReady"
	conditionTypeSetupTestReady = "SetupTestReady"
	conditionTypeSchemaReady    = "SchemaReady"

	// Standard Kubernetes condition reasons
	ConnectorReasonDeletionFailed               = "DeletionFailed"
	ConnectorReasonFinalizerUpdateFailed        = "FinalizerUpdateFailed"
	ConnectorReasonReconciliationFailed         = "ReconciliationFailed"
	ConnectorReasonSuccess                      = "ReconciledSuccessfully"
	ConnectorReasonVaultSecretsResolutionFailed = "VaultSecretsResolutionFailed"
	ConnectorReasonFivetranClientNotInitialized = "FivetranClientNotInitialized"
	ConnectorReasonValidationFailed             = "ConnectorValidationFailed"

	SetupTestsReasonReconciliationFailed              = "ReconciliationFailed"
	SetupTestsReasonReconciliationSuccess             = "ReconciledSuccessfully"
	SetupTestsReasonReconciliationSuccessWithWarnings = "ReconciledSuccessfullyWithWarnings"
	SetupTestsReasonSkipped                           = "Skipped"

	SchemaReasonReconciliationFailed  = "ReconciliationFailed"
	SchemaReasonReconciliationSuccess = "ReconciledSuccessfully"
	SchemaReasonSkipped               = "Skipped"

	SchemaNotFoundError = "NotFound_SchemaConfig"

	envFivetranVaultSecretName = "FIVETRAN_VAULT_SECRET_NAME"
	defaultVaultSecretName     = "fivetran-vault-secret"

	// Setup test status constants
	setupTestStatusPassed  = "PASSED"
	setupTestStatusSkipped = "SKIPPED"
	setupTestStatusWarning = "WARNING"
	// setupTestStatusFailed    = "FAILED"
	// setupTestStatusJobFailed = "JOB_FAILED"

	// Status messages
	msgConnectorReady                  = "Connector is ready"
	msgSetupTestsCompletedSuccessfully = "Setup tests completed successfully"
	msgSetupTestsWarningsFormat        = "Setup tests completed with warnings: %s"
	msgSetupTestsSkipped               = "Setup tests skipped"
	msgSchemaReady                     = "Schema configuration is ready"
	msgSchemaSkipped                   = "No schema configuration specified"
)

var (
	ErrFivetranClientNotInitialized    = errors.New("fivetran client is not initialized")
	ErrVaultClientInitializationFailed = errors.New("failed to initialize vault client")
	ErrSchemaMismatchAfterRetry        = errors.New("schema still mismatches CR after retry; possible schema config issue")
	ErrSetupTestsFailed                = errors.New("setup tests failed")
	ErrConnectorValidationFailed       = errors.New("connector validation failed from annotation")
)
