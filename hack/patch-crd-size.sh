#!/bin/bash
# Patch CRDs to reduce size by adding x-kubernetes-preserve-unknown-fields to PodTemplateSpec fields
# This prevents the full schema from being stored, reducing CRD size significantly
# Also makes containers optional for runnerTemplate and jobTemplate by removing it from required fields

set -e

CRD_BASE_DIR="config/crd/bases"

# Function to remove containers from required array using yq
remove_containers_from_required() {
    local crd_file="$1"
    local template_path="$2"  # e.g., "runnerTemplate" or "jobTemplate"
    
    # Remove containers from the array using yq v4 syntax
    local filtered_array=$(yq eval ".spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.${template_path}.properties.spec.required | map(select(. != \"containers\"))" "$crd_file" 2>/dev/null || echo "[]")
    
    # If the filtered array is empty, remove the required field entirely
    # Otherwise, set it to the filtered array
    if [ "$filtered_array" = "[]" ] || [ -z "$filtered_array" ]; then
        yq eval "del(.spec.versions[].schema.openAPIV3Schema.properties.spec.properties.${template_path}.properties.spec.required)" -i "$crd_file" 2>/dev/null || true
    else
        yq eval ".spec.versions[].schema.openAPIV3Schema.properties.spec.properties.${template_path}.properties.spec.required = (.spec.versions[].schema.openAPIV3Schema.properties.spec.properties.${template_path}.properties.spec.required | map(select(. != \"containers\")))" -i "$crd_file" 2>/dev/null || true
    fi
    
    # Ensure type: object is set on the spec (required when properties exist)
    yq eval ".spec.versions[].schema.openAPIV3Schema.properties.spec.properties.${template_path}.properties.spec.type = \"object\"" -i "$crd_file" 2>/dev/null || true
}

# Function to patch a CRD file
patch_crd() {
    local crd_file="$1"
    if [ ! -f "$crd_file" ]; then
        echo "CRD file not found: $crd_file"
        return 1
    fi

    echo "Patching $crd_file to reduce size and make containers optional..."

    # Use yq to add x-kubernetes-preserve-unknown-fields
    if command -v yq &> /dev/null; then
        # Add x-kubernetes-preserve-unknown-fields to PodTemplateSpec fields
        yq eval '.spec.versions[].schema.openAPIV3Schema.properties.spec.properties.listenerTemplate."x-kubernetes-preserve-unknown-fields" = true' -i "$crd_file" 2>/dev/null || true
        yq eval '.spec.versions[].schema.openAPIV3Schema.properties.spec.properties.runnerTemplate."x-kubernetes-preserve-unknown-fields" = true' -i "$crd_file" 2>/dev/null || true
        
        # Check if jobTemplate exists before patching it (only in ActRunner CRD, not ActDeployment)
        local has_job_template=$(yq eval '.spec.versions[0].schema.openAPIV3Schema.properties.spec.properties.jobTemplate' "$crd_file" 2>/dev/null || echo "null")
        if [ "$has_job_template" != "null" ] && [ -n "$has_job_template" ]; then
            yq eval '.spec.versions[].schema.openAPIV3Schema.properties.spec.properties.jobTemplate."x-kubernetes-preserve-unknown-fields" = true' -i "$crd_file" 2>/dev/null || true
            remove_containers_from_required "$crd_file" "jobTemplate"
        fi
        
        # Remove containers from required arrays
        # ActDeployment has runnerTemplate, ActRunner has jobTemplate (already handled above)
        remove_containers_from_required "$crd_file" "runnerTemplate"
        
    else
        echo "Error: yq is required but not found. Please install yq:"
        echo "  macOS: brew install yq"
        echo "  Or download from: https://github.com/mikefarah/yq"
        return 1
    fi

    echo "Patched $crd_file"
}

# Patch ActDeployment CRD
patch_crd "${CRD_BASE_DIR}/forgejo.actions.io_actdeployments.yaml"

# Patch ActRunner CRD
patch_crd "${CRD_BASE_DIR}/forgejo.actions.io_actrunners.yaml"

echo "CRD patching complete!"
