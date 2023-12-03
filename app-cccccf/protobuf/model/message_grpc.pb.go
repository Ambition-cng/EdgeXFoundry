// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             v4.25.1
// source: message.proto

package model

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	GRpcService_ModelProcess_FullMethodName = "/model.GRpcService/modelProcess"
)

// GRpcServiceClient is the client API for GRpcService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type GRpcServiceClient interface {
	ModelProcess(ctx context.Context, in *ModelProcessRequest, opts ...grpc.CallOption) (*ModelProcessResponse, error)
}

type gRpcServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewGRpcServiceClient(cc grpc.ClientConnInterface) GRpcServiceClient {
	return &gRpcServiceClient{cc}
}

func (c *gRpcServiceClient) ModelProcess(ctx context.Context, in *ModelProcessRequest, opts ...grpc.CallOption) (*ModelProcessResponse, error) {
	out := new(ModelProcessResponse)
	err := c.cc.Invoke(ctx, GRpcService_ModelProcess_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GRpcServiceServer is the server API for GRpcService service.
// All implementations must embed UnimplementedGRpcServiceServer
// for forward compatibility
type GRpcServiceServer interface {
	ModelProcess(context.Context, *ModelProcessRequest) (*ModelProcessResponse, error)
	mustEmbedUnimplementedGRpcServiceServer()
}

// UnimplementedGRpcServiceServer must be embedded to have forward compatible implementations.
type UnimplementedGRpcServiceServer struct {
}

func (UnimplementedGRpcServiceServer) ModelProcess(context.Context, *ModelProcessRequest) (*ModelProcessResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ModelProcess not implemented")
}
func (UnimplementedGRpcServiceServer) mustEmbedUnimplementedGRpcServiceServer() {}

// UnsafeGRpcServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to GRpcServiceServer will
// result in compilation errors.
type UnsafeGRpcServiceServer interface {
	mustEmbedUnimplementedGRpcServiceServer()
}

func RegisterGRpcServiceServer(s grpc.ServiceRegistrar, srv GRpcServiceServer) {
	s.RegisterService(&GRpcService_ServiceDesc, srv)
}

func _GRpcService_ModelProcess_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ModelProcessRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GRpcServiceServer).ModelProcess(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: GRpcService_ModelProcess_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GRpcServiceServer).ModelProcess(ctx, req.(*ModelProcessRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// GRpcService_ServiceDesc is the grpc.ServiceDesc for GRpcService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var GRpcService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "model.GRpcService",
	HandlerType: (*GRpcServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "modelProcess",
			Handler:    _GRpcService_ModelProcess_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "message.proto",
}