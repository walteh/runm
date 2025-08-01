// Code generated by protoc-gen-go-opaque-helpers. DO NOT EDIT.
// source: vmfuse/v1/vmfuse.proto

package vmfusev1

import (
	protovalidate "buf.build/go/protovalidate"
)

// NewMountRequest creates a new MountRequest using the builder
func NewMountRequest(b *MountRequest_builder) *MountRequest {
	return b.Build()
}

// NewMountRequestE creates a new MountRequest using the builder with validation
func NewMountRequestE(b *MountRequest_builder) (*MountRequest, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewVmConfig creates a new VmConfig using the builder
func NewVmConfig(b *VmConfig_builder) *VmConfig {
	return b.Build()
}

// NewVmConfigE creates a new VmConfig using the builder with validation
func NewVmConfigE(b *VmConfig_builder) (*VmConfig, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewMountResponse creates a new MountResponse using the builder
func NewMountResponse(b *MountResponse_builder) *MountResponse {
	return b.Build()
}

// NewMountResponseE creates a new MountResponse using the builder with validation
func NewMountResponseE(b *MountResponse_builder) (*MountResponse, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewUnmountRequest creates a new UnmountRequest using the builder
func NewUnmountRequest(b *UnmountRequest_builder) *UnmountRequest {
	return b.Build()
}

// NewUnmountRequestE creates a new UnmountRequest using the builder with validation
func NewUnmountRequestE(b *UnmountRequest_builder) (*UnmountRequest, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewListResponse creates a new ListResponse using the builder
func NewListResponse(b *ListResponse_builder) *ListResponse {
	return b.Build()
}

// NewListResponseE creates a new ListResponse using the builder with validation
func NewListResponseE(b *ListResponse_builder) (*ListResponse, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewStatusRequest creates a new StatusRequest using the builder
func NewStatusRequest(b *StatusRequest_builder) *StatusRequest {
	return b.Build()
}

// NewStatusRequestE creates a new StatusRequest using the builder with validation
func NewStatusRequestE(b *StatusRequest_builder) (*StatusRequest, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewStatusResponse creates a new StatusResponse using the builder
func NewStatusResponse(b *StatusResponse_builder) *StatusResponse {
	return b.Build()
}

// NewStatusResponseE creates a new StatusResponse using the builder with validation
func NewStatusResponseE(b *StatusResponse_builder) (*StatusResponse, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewMountInfo creates a new MountInfo using the builder
func NewMountInfo(b *MountInfo_builder) *MountInfo {
	return b.Build()
}

// NewMountInfoE creates a new MountInfo using the builder with validation
func NewMountInfoE(b *MountInfo_builder) (*MountInfo, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}
