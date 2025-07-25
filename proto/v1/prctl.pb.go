// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        (unknown)
// source: v1/prctl.proto

package runmv1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	_ "google.golang.org/protobuf/types/gofeaturespb"
	reflect "reflect"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type PrctlPidType int32

const (
	PrctlPidType_PRCTL_PID_TYPE_UNSPECIFIED PrctlPidType = 0
	PrctlPidType_PRCTL_PID_TYPE_PID         PrctlPidType = 1
	PrctlPidType_PRCTL_PID_TYPE_TGID        PrctlPidType = 2
	PrctlPidType_PRCTL_PID_TYPE_PGID        PrctlPidType = 3
)

// Enum value maps for PrctlPidType.
var (
	PrctlPidType_name = map[int32]string{
		0: "PRCTL_PID_TYPE_UNSPECIFIED",
		1: "PRCTL_PID_TYPE_PID",
		2: "PRCTL_PID_TYPE_TGID",
		3: "PRCTL_PID_TYPE_PGID",
	}
	PrctlPidType_value = map[string]int32{
		"PRCTL_PID_TYPE_UNSPECIFIED": 0,
		"PRCTL_PID_TYPE_PID":         1,
		"PRCTL_PID_TYPE_TGID":        2,
		"PRCTL_PID_TYPE_PGID":        3,
	}
)

func (x PrctlPidType) Enum() *PrctlPidType {
	p := new(PrctlPidType)
	*p = x
	return p
}

func (x PrctlPidType) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (PrctlPidType) Descriptor() protoreflect.EnumDescriptor {
	return file_v1_prctl_proto_enumTypes[0].Descriptor()
}

func (PrctlPidType) Type() protoreflect.EnumType {
	return &file_v1_prctl_proto_enumTypes[0]
}

func (x PrctlPidType) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

type CreateRequest struct {
	state              protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_PidType PrctlPidType           `protobuf:"varint,1,opt,name=pid_type,json=pidType,enum=runm.v1.PrctlPidType"`
	unknownFields      protoimpl.UnknownFields
	sizeCache          protoimpl.SizeCache
}

func (x *CreateRequest) Reset() {
	*x = CreateRequest{}
	mi := &file_v1_prctl_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *CreateRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CreateRequest) ProtoMessage() {}

func (x *CreateRequest) ProtoReflect() protoreflect.Message {
	mi := &file_v1_prctl_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *CreateRequest) GetPidType() PrctlPidType {
	if x != nil {
		return x.xxx_hidden_PidType
	}
	return PrctlPidType_PRCTL_PID_TYPE_UNSPECIFIED
}

func (x *CreateRequest) SetPidType(v PrctlPidType) {
	x.xxx_hidden_PidType = v
}

type CreateRequest_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	PidType PrctlPidType
}

func (b0 CreateRequest_builder) Build() *CreateRequest {
	m0 := &CreateRequest{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_PidType = b.PidType
	return m0
}

type CreateResponse struct {
	state              protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_GoError string                 `protobuf:"bytes,1,opt,name=go_error,json=goError"`
	unknownFields      protoimpl.UnknownFields
	sizeCache          protoimpl.SizeCache
}

func (x *CreateResponse) Reset() {
	*x = CreateResponse{}
	mi := &file_v1_prctl_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *CreateResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CreateResponse) ProtoMessage() {}

func (x *CreateResponse) ProtoReflect() protoreflect.Message {
	mi := &file_v1_prctl_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *CreateResponse) GetGoError() string {
	if x != nil {
		return x.xxx_hidden_GoError
	}
	return ""
}

func (x *CreateResponse) SetGoError(v string) {
	x.xxx_hidden_GoError = v
}

type CreateResponse_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	GoError string
}

func (b0 CreateResponse_builder) Build() *CreateResponse {
	m0 := &CreateResponse{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_GoError = b.GoError
	return m0
}

type ShareFromRequest struct {
	state              protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_Pid     uint64                 `protobuf:"varint,1,opt,name=pid"`
	xxx_hidden_PidType PrctlPidType           `protobuf:"varint,2,opt,name=pid_type,json=pidType,enum=runm.v1.PrctlPidType"`
	unknownFields      protoimpl.UnknownFields
	sizeCache          protoimpl.SizeCache
}

func (x *ShareFromRequest) Reset() {
	*x = ShareFromRequest{}
	mi := &file_v1_prctl_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ShareFromRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ShareFromRequest) ProtoMessage() {}

func (x *ShareFromRequest) ProtoReflect() protoreflect.Message {
	mi := &file_v1_prctl_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *ShareFromRequest) GetPid() uint64 {
	if x != nil {
		return x.xxx_hidden_Pid
	}
	return 0
}

func (x *ShareFromRequest) GetPidType() PrctlPidType {
	if x != nil {
		return x.xxx_hidden_PidType
	}
	return PrctlPidType_PRCTL_PID_TYPE_UNSPECIFIED
}

func (x *ShareFromRequest) SetPid(v uint64) {
	x.xxx_hidden_Pid = v
}

func (x *ShareFromRequest) SetPidType(v PrctlPidType) {
	x.xxx_hidden_PidType = v
}

type ShareFromRequest_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	Pid     uint64
	PidType PrctlPidType
}

func (b0 ShareFromRequest_builder) Build() *ShareFromRequest {
	m0 := &ShareFromRequest{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_Pid = b.Pid
	x.xxx_hidden_PidType = b.PidType
	return m0
}

type ShareFromResponse struct {
	state              protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_GoError string                 `protobuf:"bytes,1,opt,name=go_error,json=goError"`
	unknownFields      protoimpl.UnknownFields
	sizeCache          protoimpl.SizeCache
}

func (x *ShareFromResponse) Reset() {
	*x = ShareFromResponse{}
	mi := &file_v1_prctl_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ShareFromResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ShareFromResponse) ProtoMessage() {}

func (x *ShareFromResponse) ProtoReflect() protoreflect.Message {
	mi := &file_v1_prctl_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *ShareFromResponse) GetGoError() string {
	if x != nil {
		return x.xxx_hidden_GoError
	}
	return ""
}

func (x *ShareFromResponse) SetGoError(v string) {
	x.xxx_hidden_GoError = v
}

type ShareFromResponse_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	GoError string
}

func (b0 ShareFromResponse_builder) Build() *ShareFromResponse {
	m0 := &ShareFromResponse{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_GoError = b.GoError
	return m0
}

var File_v1_prctl_proto protoreflect.FileDescriptor

const file_v1_prctl_proto_rawDesc = "" +
	"\n" +
	"\x0ev1/prctl.proto\x12\arunm.v1\x1a!google/protobuf/go_features.proto\"A\n" +
	"\rCreateRequest\x120\n" +
	"\bpid_type\x18\x01 \x01(\x0e2\x15.runm.v1.PrctlPidTypeR\apidType\"+\n" +
	"\x0eCreateResponse\x12\x19\n" +
	"\bgo_error\x18\x01 \x01(\tR\agoError\"V\n" +
	"\x10ShareFromRequest\x12\x10\n" +
	"\x03pid\x18\x01 \x01(\x04R\x03pid\x120\n" +
	"\bpid_type\x18\x02 \x01(\x0e2\x15.runm.v1.PrctlPidTypeR\apidType\".\n" +
	"\x11ShareFromResponse\x12\x19\n" +
	"\bgo_error\x18\x01 \x01(\tR\agoError*x\n" +
	"\fPrctlPidType\x12\x1e\n" +
	"\x1aPRCTL_PID_TYPE_UNSPECIFIED\x10\x00\x12\x16\n" +
	"\x12PRCTL_PID_TYPE_PID\x10\x01\x12\x17\n" +
	"\x13PRCTL_PID_TYPE_TGID\x10\x02\x12\x17\n" +
	"\x13PRCTL_PID_TYPE_PGID\x10\x032\x91\x01\n" +
	"\fPrctlService\x12;\n" +
	"\x06Create\x12\x16.runm.v1.CreateRequest\x1a\x17.runm.v1.CreateResponse\"\x00\x12D\n" +
	"\tShareFrom\x12\x19.runm.v1.ShareFromRequest\x1a\x1a.runm.v1.ShareFromResponse\"\x00B\x88\x01\n" +
	"\vcom.runm.v1B\n" +
	"PrctlProtoP\x01Z&github.com/walteh/runm/proto/v1;runmv1\xa2\x02\x03RXX\xaa\x02\aRunm.V1\xca\x02\aRunm\\V1\xe2\x02\x13Runm\\V1\\GPBMetadata\xea\x02\bRunm::V1\x92\x03\a\xd2>\x02\x10\x03\b\x02b\beditionsp\xe8\a"

var file_v1_prctl_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_v1_prctl_proto_msgTypes = make([]protoimpl.MessageInfo, 4)
var file_v1_prctl_proto_goTypes = []any{
	(PrctlPidType)(0),         // 0: runm.v1.PrctlPidType
	(*CreateRequest)(nil),     // 1: runm.v1.CreateRequest
	(*CreateResponse)(nil),    // 2: runm.v1.CreateResponse
	(*ShareFromRequest)(nil),  // 3: runm.v1.ShareFromRequest
	(*ShareFromResponse)(nil), // 4: runm.v1.ShareFromResponse
}
var file_v1_prctl_proto_depIdxs = []int32{
	0, // 0: runm.v1.CreateRequest.pid_type:type_name -> runm.v1.PrctlPidType
	0, // 1: runm.v1.ShareFromRequest.pid_type:type_name -> runm.v1.PrctlPidType
	1, // 2: runm.v1.PrctlService.Create:input_type -> runm.v1.CreateRequest
	3, // 3: runm.v1.PrctlService.ShareFrom:input_type -> runm.v1.ShareFromRequest
	2, // 4: runm.v1.PrctlService.Create:output_type -> runm.v1.CreateResponse
	4, // 5: runm.v1.PrctlService.ShareFrom:output_type -> runm.v1.ShareFromResponse
	4, // [4:6] is the sub-list for method output_type
	2, // [2:4] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_v1_prctl_proto_init() }
func file_v1_prctl_proto_init() {
	if File_v1_prctl_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_v1_prctl_proto_rawDesc), len(file_v1_prctl_proto_rawDesc)),
			NumEnums:      1,
			NumMessages:   4,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_v1_prctl_proto_goTypes,
		DependencyIndexes: file_v1_prctl_proto_depIdxs,
		EnumInfos:         file_v1_prctl_proto_enumTypes,
		MessageInfos:      file_v1_prctl_proto_msgTypes,
	}.Build()
	File_v1_prctl_proto = out.File
	file_v1_prctl_proto_goTypes = nil
	file_v1_prctl_proto_depIdxs = nil
}
