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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// FivetranConnectorSpec defines the desired state of FivetranConnector.
type FivetranConnectorSpec struct {
	Connector        Connector              `json:"connector"`
	ConnectorSchemas *ConnectorSchemaConfig `json:"connectorSchemas,omitempty"`
}

// Connector defines the configuration and settings of a FivetranConnector
// +kubebuilder:validation:XValidation:rule="!(has(self.daily_sync_time) && self.daily_sync_time != '') || self.sync_frequency == 1440",message="daily_sync_time can only be specified when sync_frequency is 1440"

type Connector struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="field is immutable"
	// The unique identifier for the group within the Fivetran system
	GroupID string `json:"group_id"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="field is immutable"
	// The connector name within the Fivetran system
	Service string `json:"service"`

	// +kubebuilder:pruning:PreserveUnknownFields
	// The connector authorization parameters
	Auth *runtime.RawExtension `json:"auth,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:pruning:PreserveUnknownFields
	// The connector configuration parameters
	Config *runtime.RawExtension `json:"config"`

	// Sync settings
	// The optional parameter that defines the sync start time when the sync frequency is already set or being set by the current request to 1440.
	// +kubebuilder:validation:Pattern=`^([0-1]?[0-9]|2[0-3]):00$`
	DailySyncTime string `json:"daily_sync_time,omitempty"`
	// +kubebuilder:validation:Enum=1;5;15;30;60;120;180;360;480;720;1440
	// The connection sync frequency in minutes
	SyncFrequency int `json:"sync_frequency,omitempty"`
	// +kubebuilder:validation:Enum=auto;manual
	// The connection schedule configuration type. Supported values: auto, manual
	ScheduleType string `json:"schedule_type,omitempty"`

	// State settings
	// Specifies whether the connection is paused
	// +kubebuilder:validation:Required
	// +kubebuilder:default=false
	Paused *bool `json:"paused"`
	// Specifies whether the setup tests should be run automatically. The default value is TRUE.
	// +kubebuilder:default=true
	RunSetupTests *bool `json:"run_setup_tests,omitempty"`
	// Specifies whether the connection should be paused after the free trial period has ended
	// +kubebuilder:default=false
	PauseAfterTrial *bool `json:"pause_after_trial,omitempty"`

	// Trust settings
	// Specifies whether we should trust the certificate automatically. The default value is TRUE.
	// +kubebuilder:default=true
	TrustCertificates *bool `json:"trust_certificates,omitempty"`
	// Specifies whether we should trust the SSH fingerprint automatically. The default value is TRUE.
	// +kubebuilder:default=true
	TrustFingerprints *bool `json:"trust_fingerprints,omitempty"`

	// Data delay sensitivity
	// +kubebuilder:validation:Enum=LOW;NORMAL;HIGH;CUSTOM;SYNC_FREQUENCY
	// The level of data delay notification threshold.
	DataDelaySensitivity string `json:"data_delay_sensitivity,omitempty"`
	// Custom sync delay notification threshold in minutes.
	DataDelayThreshold int `json:"data_delay_threshold,omitempty"`

	// Networking
	// +kubebuilder:validation:Enum=Directly;PrivateLink;SshTunnel;ProxyAgent
	NetworkingMethod string `json:"networking_method,omitempty"`
	// The unique identifier for the proxy agent within the Fivetran system
	ProxyAgentID string `json:"proxy_agent_id,omitempty"`
	// The unique identifier for the self-served private link that is used by the connection
	PrivateLinkID string `json:"private_link_id,omitempty"`
	// The unique identifier for the hybrid deployment agent within the Fivetran system.
	HybridDeploymentAgentID string `json:"hybrid_deployment_agent_id,omitempty"`
}

// Schema-related types
// SchemaConfig represents a Fivetran schema configuration
type ConnectorSchemaConfig struct {
	Schemas map[string]*SchemaObject `json:"schemas,omitempty"`
	// +kubebuilder:validation:Enum=ALLOW_ALL;ALLOW_COLUMNS;BLOCK_ALL
	// The schema change handling policy. ALLOW_ALL includes all new schemas, tables, and columns. ALLOW_COLUMNS excludes new schemas and tables but includes new columns. BLOCK_ALL excludes all new schemas, tables, and columns.
	SchemaChangeHandling string `json:"schema_change_handling,omitempty"`
}

// SchemaObject represents a schema within the connector
type SchemaObject struct {
	Enabled bool                    `json:"enabled"`
	Tables  map[string]*TableObject `json:"tables,omitempty"`
}

// TableObject represents a table within a schema
type TableObject struct {
	Enabled bool                     `json:"enabled"`
	Columns map[string]*ColumnObject `json:"columns,omitempty"`
	// +kubebuilder:validation:Enum=SOFT_DELETE;HISTORY;LIVE
	// The sync mode for the table. SOFT_DELETE preserves deleted records, HISTORY maintains change history, LIVE provides real-time data.
	SyncMode string `json:"sync_mode,omitempty"`
}

// ColumnObject represents a column within a table
type ColumnObject struct {
	Enabled      bool `json:"enabled"`
	Hashed       bool `json:"hashed,omitempty"`
	IsPrimaryKey bool `json:"is_primary_key,omitempty"`
	// +kubebuilder:validation:Enum=PLAINTEXT;HASHED;ENCRYPTED
	// The masking algorithm to apply to the column data. PLAINTEXT stores data as-is, HASHED applies hashing, ENCRYPTED applies encryption.
	MaskingAlgorithm string `json:"masking_algorithm,omitempty"`
}

// FivetranConnectorStatus defines the observed state of FivetranConnector
type FivetranConnectorStatus struct {
	// ConnectorURL is the URL of the created Fivetran connector
	ConnectorURL string `json:"connectorUrl,omitempty"`
	// ConnectorID is the ID of the created Fivetran connector
	ConnectorID string `json:"connectorId,omitempty"`
	// Conditions represent the underlying resource state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// FivetranConnector is the Schema for the fivetranconnectors API.
// +kubebuilder:printcolumn:name="Connector",type=string,JSONPath=`.status.conditions[?(@.type=="ConnectorReady")].status`,priority=0
// +kubebuilder:printcolumn:name="ConnectorURL",type=string,JSONPath=`.status.connectorUrl`,priority=0
// +kubebuilder:printcolumn:name="SetupTests",type=string,JSONPath=`.status.conditions[?(@.type=="SetupTestReady")].status`,priority=1
// +kubebuilder:printcolumn:name="Schema",type=string,JSONPath=`.status.conditions[?(@.type=="SchemaReady")].status`,priority=1
// +kubebuilder:printcolumn:name="ConnectorID",type=string,JSONPath=`.status.connectorId`,priority=1
type FivetranConnector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FivetranConnectorSpec   `json:"spec,omitempty"`
	Status FivetranConnectorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FivetranConnectorList contains a list of FivetranConnector.
type FivetranConnectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FivetranConnector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FivetranConnector{}, &FivetranConnectorList{})
}
