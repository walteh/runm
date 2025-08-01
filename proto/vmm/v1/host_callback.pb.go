// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        (unknown)
// source: vmm/v1/host_callback.proto

package vmmv1

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

type ForkExecProxyRequest struct {
	state            protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_Argc  string                 `protobuf:"bytes,1,opt,name=argc"`
	xxx_hidden_Argv  []string               `protobuf:"bytes,2,rep,name=argv"`
	xxx_hidden_Env   []string               `protobuf:"bytes,3,rep,name=env"`
	xxx_hidden_Stdin []byte                 `protobuf:"bytes,4,opt,name=stdin"`
	unknownFields    protoimpl.UnknownFields
	sizeCache        protoimpl.SizeCache
}

func (x *ForkExecProxyRequest) Reset() {
	*x = ForkExecProxyRequest{}
	mi := &file_vmm_v1_host_callback_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ForkExecProxyRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ForkExecProxyRequest) ProtoMessage() {}

func (x *ForkExecProxyRequest) ProtoReflect() protoreflect.Message {
	mi := &file_vmm_v1_host_callback_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *ForkExecProxyRequest) GetArgc() string {
	if x != nil {
		return x.xxx_hidden_Argc
	}
	return ""
}

func (x *ForkExecProxyRequest) GetArgv() []string {
	if x != nil {
		return x.xxx_hidden_Argv
	}
	return nil
}

func (x *ForkExecProxyRequest) GetEnv() []string {
	if x != nil {
		return x.xxx_hidden_Env
	}
	return nil
}

func (x *ForkExecProxyRequest) GetStdin() []byte {
	if x != nil {
		return x.xxx_hidden_Stdin
	}
	return nil
}

func (x *ForkExecProxyRequest) SetArgc(v string) {
	x.xxx_hidden_Argc = v
}

func (x *ForkExecProxyRequest) SetArgv(v []string) {
	x.xxx_hidden_Argv = v
}

func (x *ForkExecProxyRequest) SetEnv(v []string) {
	x.xxx_hidden_Env = v
}

func (x *ForkExecProxyRequest) SetStdin(v []byte) {
	if v == nil {
		v = []byte{}
	}
	x.xxx_hidden_Stdin = v
}

type ForkExecProxyRequest_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	Argc  string
	Argv  []string
	Env   []string
	Stdin []byte
}

func (b0 ForkExecProxyRequest_builder) Build() *ForkExecProxyRequest {
	m0 := &ForkExecProxyRequest{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_Argc = b.Argc
	x.xxx_hidden_Argv = b.Argv
	x.xxx_hidden_Env = b.Env
	x.xxx_hidden_Stdin = b.Stdin
	return m0
}

type ForkExecProxyResponse struct {
	state               protoimpl.MessageState `protogen:"opaque.v1"`
	xxx_hidden_Stdout   []byte                 `protobuf:"bytes,1,opt,name=stdout"`
	xxx_hidden_Stderr   []byte                 `protobuf:"bytes,2,opt,name=stderr"`
	xxx_hidden_ExitCode int32                  `protobuf:"varint,3,opt,name=exit_code,json=exitCode"`
	unknownFields       protoimpl.UnknownFields
	sizeCache           protoimpl.SizeCache
}

func (x *ForkExecProxyResponse) Reset() {
	*x = ForkExecProxyResponse{}
	mi := &file_vmm_v1_host_callback_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ForkExecProxyResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ForkExecProxyResponse) ProtoMessage() {}

func (x *ForkExecProxyResponse) ProtoReflect() protoreflect.Message {
	mi := &file_vmm_v1_host_callback_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

func (x *ForkExecProxyResponse) GetStdout() []byte {
	if x != nil {
		return x.xxx_hidden_Stdout
	}
	return nil
}

func (x *ForkExecProxyResponse) GetStderr() []byte {
	if x != nil {
		return x.xxx_hidden_Stderr
	}
	return nil
}

func (x *ForkExecProxyResponse) GetExitCode() int32 {
	if x != nil {
		return x.xxx_hidden_ExitCode
	}
	return 0
}

func (x *ForkExecProxyResponse) SetStdout(v []byte) {
	if v == nil {
		v = []byte{}
	}
	x.xxx_hidden_Stdout = v
}

func (x *ForkExecProxyResponse) SetStderr(v []byte) {
	if v == nil {
		v = []byte{}
	}
	x.xxx_hidden_Stderr = v
}

func (x *ForkExecProxyResponse) SetExitCode(v int32) {
	x.xxx_hidden_ExitCode = v
}

type ForkExecProxyResponse_builder struct {
	_ [0]func() // Prevents comparability and use of unkeyed literals for the builder.

	Stdout   []byte
	Stderr   []byte
	ExitCode int32
}

func (b0 ForkExecProxyResponse_builder) Build() *ForkExecProxyResponse {
	m0 := &ForkExecProxyResponse{}
	b, x := &b0, m0
	_, _ = b, x
	x.xxx_hidden_Stdout = b.Stdout
	x.xxx_hidden_Stderr = b.Stderr
	x.xxx_hidden_ExitCode = b.ExitCode
	return m0
}

var File_vmm_v1_host_callback_proto protoreflect.FileDescriptor

const file_vmm_v1_host_callback_proto_rawDesc = "" +
	"\n" +
	"\x1avmm/v1/host_callback.proto\x12\vrunm.vmm.v1\x1a!google/protobuf/go_features.proto\"f\n" +
	"\x14ForkExecProxyRequest\x12\x12\n" +
	"\x04argc\x18\x01 \x01(\tR\x04argc\x12\x12\n" +
	"\x04argv\x18\x02 \x03(\tR\x04argv\x12\x10\n" +
	"\x03env\x18\x03 \x03(\tR\x03env\x12\x14\n" +
	"\x05stdin\x18\x04 \x01(\fR\x05stdin\"d\n" +
	"\x15ForkExecProxyResponse\x12\x16\n" +
	"\x06stdout\x18\x01 \x01(\fR\x06stdout\x12\x16\n" +
	"\x06stderr\x18\x02 \x01(\fR\x06stderr\x12\x1b\n" +
	"\texit_code\x18\x03 \x01(\x05R\bexitCode2o\n" +
	"\x13HostCallbackService\x12X\n" +
	"\rForkExecProxy\x12!.runm.vmm.v1.ForkExecProxyRequest\x1a\".runm.vmm.v1.ForkExecProxyResponse\"\x00B\xa7\x01\n" +
	"\x0fcom.runm.vmm.v1B\x11HostCallbackProtoP\x01Z)github.com/walteh/runm/proto/vmm/v1;vmmv1\xa2\x02\x03RVX\xaa\x02\vRunm.Vmm.V1\xca\x02\vRunm\\Vmm\\V1\xe2\x02\x17Runm\\Vmm\\V1\\GPBMetadata\xea\x02\rRunm::Vmm::V1\x92\x03\a\xd2>\x02\x10\x03\b\x02b\beditionsp\xe8\a"

var file_vmm_v1_host_callback_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_vmm_v1_host_callback_proto_goTypes = []any{
	(*ForkExecProxyRequest)(nil),  // 0: runm.vmm.v1.ForkExecProxyRequest
	(*ForkExecProxyResponse)(nil), // 1: runm.vmm.v1.ForkExecProxyResponse
}
var file_vmm_v1_host_callback_proto_depIdxs = []int32{
	0, // 0: runm.vmm.v1.HostCallbackService.ForkExecProxy:input_type -> runm.vmm.v1.ForkExecProxyRequest
	1, // 1: runm.vmm.v1.HostCallbackService.ForkExecProxy:output_type -> runm.vmm.v1.ForkExecProxyResponse
	1, // [1:2] is the sub-list for method output_type
	0, // [0:1] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_vmm_v1_host_callback_proto_init() }
func file_vmm_v1_host_callback_proto_init() {
	if File_vmm_v1_host_callback_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_vmm_v1_host_callback_proto_rawDesc), len(file_vmm_v1_host_callback_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_vmm_v1_host_callback_proto_goTypes,
		DependencyIndexes: file_vmm_v1_host_callback_proto_depIdxs,
		MessageInfos:      file_vmm_v1_host_callback_proto_msgTypes,
	}.Build()
	File_vmm_v1_host_callback_proto = out.File
	file_vmm_v1_host_callback_proto_goTypes = nil
	file_vmm_v1_host_callback_proto_depIdxs = nil
}
