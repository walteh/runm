// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        (unknown)
// source: vmfuse/v1/vmfuse.proto

package vmfusev1

import (
	_ "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	_ "google.golang.org/protobuf/types/gofeaturespb"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	reflect "reflect"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type MountStatus int32

const (
	MountStatus_MOUNT_STATUS_UNKNOWN         MountStatus = 0
	MountStatus_MOUNT_STATUS_CREATING        MountStatus = 1
	MountStatus_MOUNT_STATUS_STARTING_VM     MountStatus = 2
	MountStatus_MOUNT_STATUS_WAITING_FOR_NFS MountStatus = 3
	MountStatus_MOUNT_STATUS_MOUNTING_NFS    MountStatus = 4
	MountStatus_MOUNT_STATUS_ACTIVE          MountStatus = 5
	MountStatus_MOUNT_STATUS_UNMOUNTING      MountStatus = 6
	MountStatus_MOUNT_STATUS_ERROR           MountStatus = 7
)

// Enum value maps for MountStatus.
var (
	MountStatus_name = map[int32]string{
		0: "MOUNT_STATUS_UNKNOWN",
		1: "MOUNT_STATUS_CREATING",
		2: "MOUNT_STATUS_STARTING_VM",
		3: "MOUNT_STATUS_WAITING_FOR_NFS",
		4: "MOUNT_STATUS_MOUNTING_NFS",
		5: "MOUNT_STATUS_ACTIVE",
		6: "MOUNT_STATUS_UNMOUNTING",
		7: "MOUNT_STATUS_ERROR",
	}
	MountStatus_value = map[string]int32{
		"MOUNT_STATUS_UNKNOWN":         0,
		"MOUNT_STATUS_CREATING":        1,
		"MOUNT_STATUS_STARTING_VM":     2,
		"MOUNT_STATUS_WAITING_FOR_NFS": 3,
		"MOUNT_STATUS_MOUNTING_NFS":    4,
		"MOUNT_STATUS_ACTIVE":          5,
		"MOUNT_STATUS_UNMOUNTING":      6,
		"MOUNT_STATUS_ERROR":           7,
	}
)

func (x MountStatus) Enum() *MountStatus {
	p := new(MountStatus)
	*p = x
	return p
}

func (x MountStatus) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (MountStatus) Descriptor() protoreflect.EnumDescriptor {
	return file_vmfuse_v1_vmfuse_proto_enumTypes[0].Descriptor()
}

func (MountStatus) Type() protoreflect.EnumType {
	return &file_vmfuse_v1_vmfuse_proto_enumTypes[0]
}

func (x MountStatus) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

type MountRequest struct {
	state                protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_MountType string                 `protobuf:"bytes,1,opt,name=mount_type,json=mountType"`
	xxx_hidden_Sources   []string               `protobuf:"bytes,2,rep,name=sources"`
	xxx_hidden_Target    string                 `protobuf:"bytes,3,opt,name=target"`
	xxx_hidden_VmConfig  *VmConfig              `protobuf:"bytes,4,opt,name=vm_config,json=vmConfig"`
	unknownFields        protoimpl.UnknownFields
	sizeCache            protoimpl.SizeCache
}

func (x *MountRequest) Reset() {
	*x = MountRequest{}
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MountRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MountRequest) ProtoMessage() {}

func (x *MountRequest) ProtoReflect() protoreflect.Message {
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *MountRequest) GetMountType() string {
	if x != nil {
		return x.xxx_hidden_MountType
	}
	return ""
}

func (x *MountRequest) GetSources() []string {
	if x != nil {
		return x.xxx_hidden_Sources
	}
	return nil
}

func (x *MountRequest) GetTarget() string {
	if x != nil {
		return x.xxx_hidden_Target
	}
	return ""
}

func (x *MountRequest) GetVmConfig() *VmConfig {
	if x != nil {
		return x.xxx_hidden_VmConfig
	}
	return nil
}

func (x *MountRequest) SetMountType(v string) {
	x.xxx_hidden_MountType = v
}

func (x *MountRequest) SetSources(v []string) {
	x.xxx_hidden_Sources = v
}

func (x *MountRequest) SetTarget(v string) {
	x.xxx_hidden_Target = v
}

func (x *MountRequest) SetVmConfig(v *VmConfig) {
	x.xxx_hidden_VmConfig = v
}

func (x *MountRequest) HasVmConfig() bool {
	if x == nil {
		return false
	}
	return x.xxx_hidden_VmConfig != nil
}

func (x *MountRequest) ClearVmConfig() {
	x.xxx_hidden_VmConfig = nil
}

type MountRequest_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// Type of mount: "bind" or "overlay"
	MountType string
	// Source paths to mount
	// For bind: single source directory
	// For overlay: [lower, upper, work] (work is optional)
	Sources []string
	// Target path on host where mount will be available
	Target string
	// VM configuration
	VmConfig *VmConfig
}

func (b0 MountRequest_builder) Build() *MountRequest {
	m0 := &MountRequest{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_MountType = b.MountType
	x.xxx_hidden_Sources = b.Sources
	x.xxx_hidden_Target = b.Target
	x.xxx_hidden_VmConfig = b.VmConfig
	return m0
}

type VmConfig struct {
	state                     protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_MemoryMib      uint64                 `protobuf:"varint,1,opt,name=memory_mib,json=memoryMib"`
	xxx_hidden_Cpus           uint32                 `protobuf:"varint,2,opt,name=cpus"`
	xxx_hidden_TimeoutSeconds uint32                 `protobuf:"varint,3,opt,name=timeout_seconds,json=timeoutSeconds"`
	unknownFields             protoimpl.UnknownFields
	sizeCache                 protoimpl.SizeCache
}

func (x *VmConfig) Reset() {
	*x = VmConfig{}
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *VmConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*VmConfig) ProtoMessage() {}

func (x *VmConfig) ProtoReflect() protoreflect.Message {
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *VmConfig) GetMemoryMib() uint64 {
	if x != nil {
		return x.xxx_hidden_MemoryMib
	}
	return 0
}

func (x *VmConfig) GetCpus() uint32 {
	if x != nil {
		return x.xxx_hidden_Cpus
	}
	return 0
}

func (x *VmConfig) GetTimeoutSeconds() uint32 {
	if x != nil {
		return x.xxx_hidden_TimeoutSeconds
	}
	return 0
}

func (x *VmConfig) SetMemoryMib(v uint64) {
	x.xxx_hidden_MemoryMib = v
}

func (x *VmConfig) SetCpus(v uint32) {
	x.xxx_hidden_Cpus = v
}

func (x *VmConfig) SetTimeoutSeconds(v uint32) {
	x.xxx_hidden_TimeoutSeconds = v
}

type VmConfig_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// Memory allocation for VM (e.g. "512M", "1G")
	MemoryMib uint64
	// Number of CPUs for VM
	Cpus uint32
	// Timeout for VM operations in seconds
	TimeoutSeconds uint32
}

func (b0 VmConfig_builder) Build() *VmConfig {
	m0 := &VmConfig{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_MemoryMib = b.MemoryMib
	x.xxx_hidden_Cpus = b.Cpus
	x.xxx_hidden_TimeoutSeconds = b.TimeoutSeconds
	return m0
}

type MountResponse struct {
	state              protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_MountId string                 `protobuf:"bytes,1,opt,name=mount_id,json=mountId"`
	xxx_hidden_VmId    string                 `protobuf:"bytes,2,opt,name=vm_id,json=vmId"`
	xxx_hidden_Target  string                 `protobuf:"bytes,3,opt,name=target"`
	xxx_hidden_Status  MountStatus            `protobuf:"varint,4,opt,name=status,enum=github.com.walteh.runm.proto.vmfuse.v1.MountStatus"`
	unknownFields      protoimpl.UnknownFields
	sizeCache          protoimpl.SizeCache
}

func (x *MountResponse) Reset() {
	*x = MountResponse{}
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MountResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MountResponse) ProtoMessage() {}

func (x *MountResponse) ProtoReflect() protoreflect.Message {
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *MountResponse) GetMountId() string {
	if x != nil {
		return x.xxx_hidden_MountId
	}
	return ""
}

func (x *MountResponse) GetVmId() string {
	if x != nil {
		return x.xxx_hidden_VmId
	}
	return ""
}

func (x *MountResponse) GetTarget() string {
	if x != nil {
		return x.xxx_hidden_Target
	}
	return ""
}

func (x *MountResponse) GetStatus() MountStatus {
	if x != nil {
		return x.xxx_hidden_Status
	}
	return MountStatus_MOUNT_STATUS_UNKNOWN
}

func (x *MountResponse) SetMountId(v string) {
	x.xxx_hidden_MountId = v
}

func (x *MountResponse) SetVmId(v string) {
	x.xxx_hidden_VmId = v
}

func (x *MountResponse) SetTarget(v string) {
	x.xxx_hidden_Target = v
}

func (x *MountResponse) SetStatus(v MountStatus) {
	x.xxx_hidden_Status = v
}

type MountResponse_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// Unique identifier for this mount
	MountId string
	// VM identifier
	VmId string
	// Target path where mount is available
	Target string
	// Mount status
	Status MountStatus
}

func (b0 MountResponse_builder) Build() *MountResponse {
	m0 := &MountResponse{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_MountId = b.MountId
	x.xxx_hidden_VmId = b.VmId
	x.xxx_hidden_Target = b.Target
	x.xxx_hidden_Status = b.Status
	return m0
}

type UnmountRequest struct {
	state             protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_Target string                 `protobuf:"bytes,1,opt,name=target"`
	unknownFields     protoimpl.UnknownFields
	sizeCache         protoimpl.SizeCache
}

func (x *UnmountRequest) Reset() {
	*x = UnmountRequest{}
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *UnmountRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*UnmountRequest) ProtoMessage() {}

func (x *UnmountRequest) ProtoReflect() protoreflect.Message {
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *UnmountRequest) GetTarget() string {
	if x != nil {
		return x.xxx_hidden_Target
	}
	return ""
}

func (x *UnmountRequest) SetTarget(v string) {
	x.xxx_hidden_Target = v
}

type UnmountRequest_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// Target path to unmount
	Target string
}

func (b0 UnmountRequest_builder) Build() *UnmountRequest {
	m0 := &UnmountRequest{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_Target = b.Target
	return m0
}

type ListResponse struct {
	state             protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_Mounts *[]*MountInfo          `protobuf:"bytes,1,rep,name=mounts"`
	unknownFields     protoimpl.UnknownFields
	sizeCache         protoimpl.SizeCache
}

func (x *ListResponse) Reset() {
	*x = ListResponse{}
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListResponse) ProtoMessage() {}

func (x *ListResponse) ProtoReflect() protoreflect.Message {
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *ListResponse) GetMounts() []*MountInfo {
	if x != nil {
		if x.xxx_hidden_Mounts != nil {
			return *x.xxx_hidden_Mounts
		}
	}
	return nil
}

func (x *ListResponse) SetMounts(v []*MountInfo) {
	x.xxx_hidden_Mounts = &v
}

type ListResponse_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	Mounts []*MountInfo
}

func (b0 ListResponse_builder) Build() *ListResponse {
	m0 := &ListResponse{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_Mounts = &b.Mounts
	return m0
}

type StatusRequest struct {
	state                 protoimpl.MessageState     `protogen:"opaque.v1"`
	xxx_hidden_Identifier isStatusRequest_Identifier `protobuf_oneof:"identifier"`
	unknownFields         protoimpl.UnknownFields
	sizeCache             protoimpl.SizeCache
}

func (x *StatusRequest) Reset() {
	*x = StatusRequest{}
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StatusRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StatusRequest) ProtoMessage() {}

func (x *StatusRequest) ProtoReflect() protoreflect.Message {
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *StatusRequest) GetMountId() string {
	if x != nil {
		if x, ok := x.xxx_hidden_Identifier.(*statusRequest_MountId); ok {
			return x.MountId
		}
	}
	return ""
}

func (x *StatusRequest) GetTarget() string {
	if x != nil {
		if x, ok := x.xxx_hidden_Identifier.(*statusRequest_Target); ok {
			return x.Target
		}
	}
	return ""
}

func (x *StatusRequest) SetMountId(v string) {
	x.xxx_hidden_Identifier = &statusRequest_MountId{v}
}

func (x *StatusRequest) SetTarget(v string) {
	x.xxx_hidden_Identifier = &statusRequest_Target{v}
}

func (x *StatusRequest) HasIdentifier() bool {
	if x == nil {
		return false
	}
	return x.xxx_hidden_Identifier != nil
}

func (x *StatusRequest) HasMountId() bool {
	if x == nil {
		return false
	}
	_, ok := x.xxx_hidden_Identifier.(*statusRequest_MountId)
	return ok
}

func (x *StatusRequest) HasTarget() bool {
	if x == nil {
		return false
	}
	_, ok := x.xxx_hidden_Identifier.(*statusRequest_Target)
	return ok
}

func (x *StatusRequest) ClearIdentifier() {
	x.xxx_hidden_Identifier = nil
}

func (x *StatusRequest) ClearMountId() {
	if _, ok := x.xxx_hidden_Identifier.(*statusRequest_MountId); ok {
		x.xxx_hidden_Identifier = nil
	}
}

func (x *StatusRequest) ClearTarget() {
	if _, ok := x.xxx_hidden_Identifier.(*statusRequest_Target); ok {
		x.xxx_hidden_Identifier = nil
	}
}

const StatusRequest_Identifier_not_set_case case_StatusRequest_Identifier = 0
const StatusRequest_MountId_case case_StatusRequest_Identifier = 1
const StatusRequest_Target_case case_StatusRequest_Identifier = 2

func (x *StatusRequest) WhichIdentifier() case_StatusRequest_Identifier {
	if x == nil {
		return StatusRequest_Identifier_not_set_case
	}
	switch x.xxx_hidden_Identifier.(type) {
	case *statusRequest_MountId:
		return StatusRequest_MountId_case
	case *statusRequest_Target:
		return StatusRequest_Target_case
	default:
		return StatusRequest_Identifier_not_set_case
	}
}

type StatusRequest_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// Either mount_id or target must be specified

	// Fields of oneof xxx_hidden_Identifier:
	MountId *string
	Target  *string
	// -- end of xxx_hidden_Identifier
}

func (b0 StatusRequest_builder) Build() *StatusRequest {
	m0 := &StatusRequest{}
	b, x := &b0, m0
	_, _ = b, x
	if b.MountId != nil {
		x.xxx_hidden_Identifier = &statusRequest_MountId{*b.MountId}
	}
	if b.Target != nil {
		x.xxx_hidden_Identifier = &statusRequest_Target{*b.Target}
	}
	return m0
}

type case_StatusRequest_Identifier protoreflect.FieldNumber

func (x case_StatusRequest_Identifier) String() string {
	md := file_vmfuse_v1_vmfuse_proto_msgTypes[5].Descriptor()
	if x == 0 {
		return "not set"
	}
	return protoimpl.X.MessageFieldStringOf(md, protoreflect.FieldNumber(x))
}

type isStatusRequest_Identifier interface {
	isStatusRequest_Identifier()
}

type statusRequest_MountId struct {
	MountId string `protobuf:"bytes,1,opt,name=mount_id,json=mountId,oneof"`
}

type statusRequest_Target struct {
	Target string `protobuf:"bytes,2,opt,name=target,oneof"`
}

func (*statusRequest_MountId) isStatusRequest_Identifier() {}

func (*statusRequest_Target) isStatusRequest_Identifier() {}

type StatusResponse struct {
	state                protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_MountInfo *MountInfo             `protobuf:"bytes,1,opt,name=mount_info,json=mountInfo"`
	unknownFields        protoimpl.UnknownFields
	sizeCache            protoimpl.SizeCache
}

func (x *StatusResponse) Reset() {
	*x = StatusResponse{}
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[6]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StatusResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StatusResponse) ProtoMessage() {}

func (x *StatusResponse) ProtoReflect() protoreflect.Message {
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[6]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *StatusResponse) GetMountInfo() *MountInfo {
	if x != nil {
		return x.xxx_hidden_MountInfo
	}
	return nil
}

func (x *StatusResponse) SetMountInfo(v *MountInfo) {
	x.xxx_hidden_MountInfo = v
}

func (x *StatusResponse) HasMountInfo() bool {
	if x == nil {
		return false
	}
	return x.xxx_hidden_MountInfo != nil
}

func (x *StatusResponse) ClearMountInfo() {
	x.xxx_hidden_MountInfo = nil
}

type StatusResponse_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	MountInfo *MountInfo
}

func (b0 StatusResponse_builder) Build() *StatusResponse {
	m0 := &StatusResponse{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_MountInfo = b.MountInfo
	return m0
}

type MountInfo struct {
	state                   protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_MountId      string                 `protobuf:"bytes,1,opt,name=mount_id,json=mountId"`
	xxx_hidden_VmId         string                 `protobuf:"bytes,2,opt,name=vm_id,json=vmId"`
	xxx_hidden_MountType    string                 `protobuf:"bytes,3,opt,name=mount_type,json=mountType"`
	xxx_hidden_Sources      []string               `protobuf:"bytes,4,rep,name=sources"`
	xxx_hidden_Target       string                 `protobuf:"bytes,5,opt,name=target"`
	xxx_hidden_Status       MountStatus            `protobuf:"varint,6,opt,name=status,enum=github.com.walteh.runm.proto.vmfuse.v1.MountStatus"`
	xxx_hidden_NfsHostPort  uint32                 `protobuf:"varint,7,opt,name=nfs_host_port,json=nfsHostPort"`
	xxx_hidden_CreatedAt    int64                  `protobuf:"varint,8,opt,name=created_at,json=createdAt"`
	xxx_hidden_ErrorMessage string                 `protobuf:"bytes,9,opt,name=error_message,json=errorMessage"`
	unknownFields           protoimpl.UnknownFields
	sizeCache               protoimpl.SizeCache
}

func (x *MountInfo) Reset() {
	*x = MountInfo{}
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[7]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *MountInfo) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MountInfo) ProtoMessage() {}

func (x *MountInfo) ProtoReflect() protoreflect.Message {
	mi := &file_vmfuse_v1_vmfuse_proto_msgTypes[7]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *MountInfo) GetMountId() string {
	if x != nil {
		return x.xxx_hidden_MountId
	}
	return ""
}

func (x *MountInfo) GetVmId() string {
	if x != nil {
		return x.xxx_hidden_VmId
	}
	return ""
}

func (x *MountInfo) GetMountType() string {
	if x != nil {
		return x.xxx_hidden_MountType
	}
	return ""
}

func (x *MountInfo) GetSources() []string {
	if x != nil {
		return x.xxx_hidden_Sources
	}
	return nil
}

func (x *MountInfo) GetTarget() string {
	if x != nil {
		return x.xxx_hidden_Target
	}
	return ""
}

func (x *MountInfo) GetStatus() MountStatus {
	if x != nil {
		return x.xxx_hidden_Status
	}
	return MountStatus_MOUNT_STATUS_UNKNOWN
}

func (x *MountInfo) GetNfsHostPort() uint32 {
	if x != nil {
		return x.xxx_hidden_NfsHostPort
	}
	return 0
}

func (x *MountInfo) GetCreatedAt() int64 {
	if x != nil {
		return x.xxx_hidden_CreatedAt
	}
	return 0
}

func (x *MountInfo) GetErrorMessage() string {
	if x != nil {
		return x.xxx_hidden_ErrorMessage
	}
	return ""
}

func (x *MountInfo) SetMountId(v string) {
	x.xxx_hidden_MountId = v
}

func (x *MountInfo) SetVmId(v string) {
	x.xxx_hidden_VmId = v
}

func (x *MountInfo) SetMountType(v string) {
	x.xxx_hidden_MountType = v
}

func (x *MountInfo) SetSources(v []string) {
	x.xxx_hidden_Sources = v
}

func (x *MountInfo) SetTarget(v string) {
	x.xxx_hidden_Target = v
}

func (x *MountInfo) SetStatus(v MountStatus) {
	x.xxx_hidden_Status = v
}

func (x *MountInfo) SetNfsHostPort(v uint32) {
	x.xxx_hidden_NfsHostPort = v
}

func (x *MountInfo) SetCreatedAt(v int64) {
	x.xxx_hidden_CreatedAt = v
}

func (x *MountInfo) SetErrorMessage(v string) {
	x.xxx_hidden_ErrorMessage = v
}

type MountInfo_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	// Unique identifier for this mount
	MountId string
	// VM identifier
	VmId string
	// Mount configuration
	MountType string
	Sources   []string
	Target    string
	// Status information
	Status MountStatus
	// VM IP address (for debugging)
	NfsHostPort uint32
	// Creation timestamp (unix nano)
	CreatedAt int64
	// Error message if status is ERROR
	ErrorMessage string
}

func (b0 MountInfo_builder) Build() *MountInfo {
	m0 := &MountInfo{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_MountId = b.MountId
	x.xxx_hidden_VmId = b.VmId
	x.xxx_hidden_MountType = b.MountType
	x.xxx_hidden_Sources = b.Sources
	x.xxx_hidden_Target = b.Target
	x.xxx_hidden_Status = b.Status
	x.xxx_hidden_NfsHostPort = b.NfsHostPort
	x.xxx_hidden_CreatedAt = b.CreatedAt
	x.xxx_hidden_ErrorMessage = b.ErrorMessage
	return m0
}

var File_vmfuse_v1_vmfuse_proto protoreflect.FileDescriptor

const file_vmfuse_v1_vmfuse_proto_rawDesc = "" +
	"\n" +
	"\x16vmfuse/v1/vmfuse.proto\x12&github.com.walteh.runm.proto.vmfuse.v1\x1a\x1bbuf/validate/validate.proto\x1a\x1bgoogle/protobuf/empty.proto\x1a!google/protobuf/go_features.proto\"\xd7\x01\n" +
	"\fMountRequest\x123\n" +
	"\n" +
	"mount_type\x18\x01 \x01(\tB\x14\xbaH\x11r\x0fR\x04bindR\aoverlayR\tmountType\x12\"\n" +
	"\asources\x18\x02 \x03(\tB\b\xbaH\x05\x92\x01\x02\b\x01R\asources\x12\x1f\n" +
	"\x06target\x18\x03 \x01(\tB\a\xbaH\x04r\x02\x10\x01R\x06target\x12M\n" +
	"\tvm_config\x18\x04 \x01(\v20.github.com.walteh.runm.proto.vmfuse.v1.VmConfigR\bvmConfig\"z\n" +
	"\bVmConfig\x12\x1d\n" +
	"\n" +
	"memory_mib\x18\x01 \x01(\x04R\tmemoryMib\x12\x1d\n" +
	"\x04cpus\x18\x02 \x01(\rB\t\xbaH\x06*\x04\x18\x10(\x01R\x04cpus\x120\n" +
	"\x0ftimeout_seconds\x18\x03 \x01(\rB\a\xbaH\x04*\x02(\x1eR\x0etimeoutSeconds\"\xa4\x01\n" +
	"\rMountResponse\x12\x19\n" +
	"\bmount_id\x18\x01 \x01(\tR\amountId\x12\x13\n" +
	"\x05vm_id\x18\x02 \x01(\tR\x04vmId\x12\x16\n" +
	"\x06target\x18\x03 \x01(\tR\x06target\x12K\n" +
	"\x06status\x18\x04 \x01(\x0e23.github.com.walteh.runm.proto.vmfuse.v1.MountStatusR\x06status\"1\n" +
	"\x0eUnmountRequest\x12\x1f\n" +
	"\x06target\x18\x01 \x01(\tB\a\xbaH\x04r\x02\x10\x01R\x06target\"Y\n" +
	"\fListResponse\x12I\n" +
	"\x06mounts\x18\x01 \x03(\v21.github.com.walteh.runm.proto.vmfuse.v1.MountInfoR\x06mounts\"]\n" +
	"\rStatusRequest\x12\x1b\n" +
	"\bmount_id\x18\x01 \x01(\tH\x00R\amountId\x12!\n" +
	"\x06target\x18\x02 \x01(\tB\a\xbaH\x04r\x02\x10\x01H\x00R\x06targetB\f\n" +
	"\n" +
	"identifier\"b\n" +
	"\x0eStatusResponse\x12P\n" +
	"\n" +
	"mount_info\x18\x01 \x01(\v21.github.com.walteh.runm.proto.vmfuse.v1.MountInfoR\tmountInfo\"\xc1\x02\n" +
	"\tMountInfo\x12\x19\n" +
	"\bmount_id\x18\x01 \x01(\tR\amountId\x12\x13\n" +
	"\x05vm_id\x18\x02 \x01(\tR\x04vmId\x12\x1d\n" +
	"\n" +
	"mount_type\x18\x03 \x01(\tR\tmountType\x12\x18\n" +
	"\asources\x18\x04 \x03(\tR\asources\x12\x16\n" +
	"\x06target\x18\x05 \x01(\tR\x06target\x12K\n" +
	"\x06status\x18\x06 \x01(\x0e23.github.com.walteh.runm.proto.vmfuse.v1.MountStatusR\x06status\x12\"\n" +
	"\rnfs_host_port\x18\a \x01(\rR\vnfsHostPort\x12\x1d\n" +
	"\n" +
	"created_at\x18\b \x01(\x03R\tcreatedAt\x12#\n" +
	"\rerror_message\x18\t \x01(\tR\ferrorMessage*\xef\x01\n" +
	"\vMountStatus\x12\x18\n" +
	"\x14MOUNT_STATUS_UNKNOWN\x10\x00\x12\x19\n" +
	"\x15MOUNT_STATUS_CREATING\x10\x01\x12\x1c\n" +
	"\x18MOUNT_STATUS_STARTING_VM\x10\x02\x12 \n" +
	"\x1cMOUNT_STATUS_WAITING_FOR_NFS\x10\x03\x12\x1d\n" +
	"\x19MOUNT_STATUS_MOUNTING_NFS\x10\x04\x12\x17\n" +
	"\x13MOUNT_STATUS_ACTIVE\x10\x05\x12\x1b\n" +
	"\x17MOUNT_STATUS_UNMOUNTING\x10\x06\x12\x16\n" +
	"\x12MOUNT_STATUS_ERROR\x10\a2\xaf\x03\n" +
	"\rVmfuseService\x12t\n" +
	"\x05Mount\x124.github.com.walteh.runm.proto.vmfuse.v1.MountRequest\x1a5.github.com.walteh.runm.proto.vmfuse.v1.MountResponse\x12Y\n" +
	"\aUnmount\x126.github.com.walteh.runm.proto.vmfuse.v1.UnmountRequest\x1a\x16.google.protobuf.Empty\x12T\n" +
	"\x04List\x12\x16.google.protobuf.Empty\x1a4.github.com.walteh.runm.proto.vmfuse.v1.ListResponse\x12w\n" +
	"\x06Status\x125.github.com.walteh.runm.proto.vmfuse.v1.StatusRequest\x1a6.github.com.walteh.runm.proto.vmfuse.v1.StatusResponseB\xb5\x02\n" +
	"*com.github.com.walteh.runm.proto.vmfuse.v1B\vVmfuseProtoP\x01Z/github.com/walteh/runm/proto/vmfuse/v1;vmfusev1\xa2\x02\x06GCWRPV\xaa\x02&Github.Com.Walteh.Runm.Proto.Vmfuse.V1\xca\x02&Github\\Com\\Walteh\\Runm\\Proto\\Vmfuse\\V1\xe2\x022Github\\Com\\Walteh\\Runm\\Proto\\Vmfuse\\V1\\GPBMetadata\xea\x02,Github::Com::Walteh::Runm::Proto::Vmfuse::V1\x92\x03\a\xd2>\x02\x10\x03\b\x02b\beditionsp\xe8\a"

var file_vmfuse_v1_vmfuse_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_vmfuse_v1_vmfuse_proto_msgTypes = make([]protoimpl.MessageInfo, 8)
var file_vmfuse_v1_vmfuse_proto_goTypes = []any{
	(MountStatus)(0),       // 0: github.com.walteh.runm.proto.vmfuse.v1.MountStatus
	(*MountRequest)(nil),   // 1: github.com.walteh.runm.proto.vmfuse.v1.MountRequest
	(*VmConfig)(nil),       // 2: github.com.walteh.runm.proto.vmfuse.v1.VmConfig
	(*MountResponse)(nil),  // 3: github.com.walteh.runm.proto.vmfuse.v1.MountResponse
	(*UnmountRequest)(nil), // 4: github.com.walteh.runm.proto.vmfuse.v1.UnmountRequest
	(*ListResponse)(nil),   // 5: github.com.walteh.runm.proto.vmfuse.v1.ListResponse
	(*StatusRequest)(nil),  // 6: github.com.walteh.runm.proto.vmfuse.v1.StatusRequest
	(*StatusResponse)(nil), // 7: github.com.walteh.runm.proto.vmfuse.v1.StatusResponse
	(*MountInfo)(nil),      // 8: github.com.walteh.runm.proto.vmfuse.v1.MountInfo
	(*emptypb.Empty)(nil),  // 9: google.protobuf.Empty
}
var file_vmfuse_v1_vmfuse_proto_depIdxs = []int32{
	2, // 0: github.com.walteh.runm.proto.vmfuse.v1.MountRequest.vm_config:type_name -> github.com.walteh.runm.proto.vmfuse.v1.VmConfig
	0, // 1: github.com.walteh.runm.proto.vmfuse.v1.MountResponse.status:type_name -> github.com.walteh.runm.proto.vmfuse.v1.MountStatus
	8, // 2: github.com.walteh.runm.proto.vmfuse.v1.ListResponse.mounts:type_name -> github.com.walteh.runm.proto.vmfuse.v1.MountInfo
	8, // 3: github.com.walteh.runm.proto.vmfuse.v1.StatusResponse.mount_info:type_name -> github.com.walteh.runm.proto.vmfuse.v1.MountInfo
	0, // 4: github.com.walteh.runm.proto.vmfuse.v1.MountInfo.status:type_name -> github.com.walteh.runm.proto.vmfuse.v1.MountStatus
	1, // 5: github.com.walteh.runm.proto.vmfuse.v1.VmfuseService.Mount:input_type -> github.com.walteh.runm.proto.vmfuse.v1.MountRequest
	4, // 6: github.com.walteh.runm.proto.vmfuse.v1.VmfuseService.Unmount:input_type -> github.com.walteh.runm.proto.vmfuse.v1.UnmountRequest
	9, // 7: github.com.walteh.runm.proto.vmfuse.v1.VmfuseService.List:input_type -> google.protobuf.Empty
	6, // 8: github.com.walteh.runm.proto.vmfuse.v1.VmfuseService.Status:input_type -> github.com.walteh.runm.proto.vmfuse.v1.StatusRequest
	3, // 9: github.com.walteh.runm.proto.vmfuse.v1.VmfuseService.Mount:output_type -> github.com.walteh.runm.proto.vmfuse.v1.MountResponse
	9, // 10: github.com.walteh.runm.proto.vmfuse.v1.VmfuseService.Unmount:output_type -> google.protobuf.Empty
	5, // 11: github.com.walteh.runm.proto.vmfuse.v1.VmfuseService.List:output_type -> github.com.walteh.runm.proto.vmfuse.v1.ListResponse
	7, // 12: github.com.walteh.runm.proto.vmfuse.v1.VmfuseService.Status:output_type -> github.com.walteh.runm.proto.vmfuse.v1.StatusResponse
	9, // [9:13] is the sub-list for method output_type
	5, // [5:9] is the sub-list for method input_type
	5, // [5:5] is the sub-list for extension type_name
	5, // [5:5] is the sub-list for extension extendee
	0, // [0:5] is the sub-list for field type_name
}

func init() { file_vmfuse_v1_vmfuse_proto_init() }
func file_vmfuse_v1_vmfuse_proto_init() {
	if File_vmfuse_v1_vmfuse_proto != nil {
		return
	}
	file_vmfuse_v1_vmfuse_proto_msgTypes[5].OneofWrappers = []any{
		(*statusRequest_MountId)(nil),
		(*statusRequest_Target)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_vmfuse_v1_vmfuse_proto_rawDesc), len(file_vmfuse_v1_vmfuse_proto_rawDesc)),
			NumEnums:      1,
			NumMessages:   8,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_vmfuse_v1_vmfuse_proto_goTypes,
		DependencyIndexes: file_vmfuse_v1_vmfuse_proto_depIdxs,
		EnumInfos:         file_vmfuse_v1_vmfuse_proto_enumTypes,
		MessageInfos:      file_vmfuse_v1_vmfuse_proto_msgTypes,
	}.Build()
	File_vmfuse_v1_vmfuse_proto = out.File
	file_vmfuse_v1_vmfuse_proto_goTypes = nil
	file_vmfuse_v1_vmfuse_proto_depIdxs = nil
}
