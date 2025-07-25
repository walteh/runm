// Code generated by protoc-gen-go-opaque-helpers. DO NOT EDIT.
// source: vmm/v1/guest_management.proto

package vmmv1

import (
	protovalidate "buf.build/go/protovalidate"
)

// NewGuestTimeSyncRequest creates a new GuestTimeSyncRequest using the builder
func NewGuestTimeSyncRequest(b *GuestTimeSyncRequest_builder) *GuestTimeSyncRequest {
	return b.Build()
}

// NewGuestTimeSyncRequestE creates a new GuestTimeSyncRequest using the builder with validation
func NewGuestTimeSyncRequestE(b *GuestTimeSyncRequest_builder) (*GuestTimeSyncRequest, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewGuestTimeSyncResponse creates a new GuestTimeSyncResponse using the builder
func NewGuestTimeSyncResponse(b *GuestTimeSyncResponse_builder) *GuestTimeSyncResponse {
	return b.Build()
}

// NewGuestTimeSyncResponseE creates a new GuestTimeSyncResponse using the builder with validation
func NewGuestTimeSyncResponseE(b *GuestTimeSyncResponse_builder) (*GuestTimeSyncResponse, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewGuestReadinessRequest creates a new GuestReadinessRequest using the builder
func NewGuestReadinessRequest(b *GuestReadinessRequest_builder) *GuestReadinessRequest {
	return b.Build()
}

// NewGuestReadinessRequestE creates a new GuestReadinessRequest using the builder with validation
func NewGuestReadinessRequestE(b *GuestReadinessRequest_builder) (*GuestReadinessRequest, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewGuestReadinessResponse creates a new GuestReadinessResponse using the builder
func NewGuestReadinessResponse(b *GuestReadinessResponse_builder) *GuestReadinessResponse {
	return b.Build()
}

// NewGuestReadinessResponseE creates a new GuestReadinessResponse using the builder with validation
func NewGuestReadinessResponseE(b *GuestReadinessResponse_builder) (*GuestReadinessResponse, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewGuestRunCommandRequest creates a new GuestRunCommandRequest using the builder
func NewGuestRunCommandRequest(b *GuestRunCommandRequest_builder) *GuestRunCommandRequest {
	return b.Build()
}

// NewGuestRunCommandRequestE creates a new GuestRunCommandRequest using the builder with validation
func NewGuestRunCommandRequestE(b *GuestRunCommandRequest_builder) (*GuestRunCommandRequest, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// NewGuestRunCommandResponse creates a new GuestRunCommandResponse using the builder
func NewGuestRunCommandResponse(b *GuestRunCommandResponse_builder) *GuestRunCommandResponse {
	return b.Build()
}

// NewGuestRunCommandResponseE creates a new GuestRunCommandResponse using the builder with validation
func NewGuestRunCommandResponseE(b *GuestRunCommandResponse_builder) (*GuestRunCommandResponse, error) {
	m := b.Build()
	if err := protovalidate.Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}
