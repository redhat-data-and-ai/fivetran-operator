# FivetranConnector Reconciliation Sequence Diagram

This high-level sequence diagram shows the main flow of the FivetranConnector reconciliation process, focusing on the key phases and external system interactions.

```mermaid
sequenceDiagram
    participant K8s as Kubernetes API
    participant R as FivetranConnectorReconciler
    participant V as Vault
    participant F as Fivetran API

    %% 1. Initial Setup
    K8s->>R: Reconcile Event
    R->>K8s: Get FivetranConnector Resource
    K8s-->>R: FivetranConnector Spec

    %% 2. Handle Deletion
    alt Connector is being deleted
        R->>F: Delete Fivetran Connector
        F-->>R: Deletion Success
        R->>K8s: Remove Finalizer & Update Status
        R-->>K8s: Return (Reconciliation Complete)
    end

    %% 3. Initialize Dependencies
    R->>V: Initialize Vault Client (if needed)
    V-->>R: Vault Client Ready
    R->>K8s: Add Finalizer (if missing)

    %% 4. Check if Work Needed
    R->>R: Determine What to Reconcile
    Note over R: Check for changes in config/schema<br/>or force reconcile annotation

    alt No changes detected
        R-->>K8s: Return (No Action Needed)
    end

    %% 5. Resolve Secrets
    R->>V: Resolve vault:// references
    V-->>R: Resolved Secrets

    %% 6. Connector Reconciliation
    alt Connector needs reconciliation
        alt Connector doesn't exist
            R->>F: Create New Connector
            F-->>R: Connector ID
        else Connector exists
            R->>F: Update Existing Connector
            F-->>R: Update Success
        end
        
        R->>K8s: Update Status & Annotations
    end

    %% 7. Setup Tests (Optional)
    alt Setup tests enabled
        R->>F: Run Setup Tests
        F-->>R: Test Results
        R->>K8s: Update Test Status
    end

    %% 8. Schema Configuration (Optional)
    alt Schema config provided
        R->>F: Get Current Schema
        F-->>R: Schema Details
        
        alt Schema needs update
            R->>F: Reload & Update Schema
            F-->>R: Schema Updated
        end
        
        R->>K8s: Update Schema Status
    end

    %% 9. Final Steps
    R->>K8s: Clean up annotations
    R->>K8s: Update Final Status
    R-->>K8s: Return (Reconciliation Complete)
```

## Reconciliation Flow Overview

The FivetranConnector reconciliation follows these **9 main phases**:

### 1. **Initial Setup**
- Controller receives reconcile event from Kubernetes
- Fetches the FivetranConnector resource specification

### 2. **Deletion Handling** 
- If connector is being deleted, removes it from Fivetran and cleans up
- Early exit after successful deletion

### 3. **Initialize Dependencies**
- Sets up Vault client for secret management (if needed)
- Ensures finalizer is present to handle cleanup

### 4. **Determine Work Needed**
- Checks if configuration or schema has changed
- Supports force reconcile via annotation
- Skips work if no changes detected

### 5. **Secret Resolution**
- Resolves any `vault:path#key` references in connector config/auth
- Retrieves secrets securely from Vault

### 6. **Connector Reconciliation**
- **Creates** new connector in Fivetran (if doesn't exist)
- **Updates** existing connector configuration (if changed)
- Updates Kubernetes status with connector details

### 7. **Setup Tests** _(Optional)_
- Runs Fivetran setup tests to validate connectivity
- Reports test results in connector status

### 8. **Schema Configuration** _(Optional)_
- Manages table/column selection and sync settings
- Reloads schema from source if needed
- Applies user-defined schema configuration

### 9. **Final Steps**
- Removes temporary annotations and labels
- Updates final status conditions
- Completes reconciliation cycle

## Key Features

- **Smart Change Detection**: Only reconciles when changes are detected
- **Secure Secret Management**: Integration with Vault for credentials
- **Error Handling**: Proper status reporting and retry logic
