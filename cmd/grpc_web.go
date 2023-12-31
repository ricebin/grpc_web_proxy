package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"github.com/mwitkow/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func main() {
	portFlag := flag.Int("port", 8080, "")
	backendFlag := flag.String("backend", "", "")

	flag.Parse()

	port := *portFlag
	backend := *backendFlag

	if backend == "" {
		log.Fatalf("missing backend flag")
	}

	log.Printf("proxying to: %s", backend)

	codec := proxy.Codec()
	backendConn := dialBackendOrFail(backend, codec)

	grpcServer := buildGrpcProxyServer(backendConn, codec)

	options := []grpcweb.Option{
		grpcweb.WithCorsForRegisteredEndpointsOnly(false),
		grpcweb.WithOriginFunc(makeHttpOriginFunc()),
	}

	wrappedGrpc := grpcweb.WrapServer(grpcServer, options...)

	serveMux := http.NewServeMux()
	serveMux.Handle("/", wrappedGrpc)

	hs := &http.Server{
		Handler: serveMux,
	}

	log.Printf("grpc web proxy listening at: %d", port)
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed listening on port: %d, error : %v", port, err)
	}

	if err := hs.Serve(listener); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func buildGrpcProxyServer(backendConn *grpc.ClientConn, codec grpc.Codec) *grpc.Server {
	// gRPC proxy logic.
	director := func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		mdCopy := md.Copy()
		// If this header is present in the request from the web client,
		// the actual connection to the backend will not be established.
		// https://github.com/improbable-eng/grpc-web/issues/568
		delete(mdCopy, "connection")
		outCtx := metadata.NewOutgoingContext(ctx, mdCopy)
		return outCtx, backendConn, nil
	}

	// Server with logging and monitoring enabled.
	return grpc.NewServer(
		grpc.CustomCodec(codec), // needed for proxy to function.
		grpc.UnknownServiceHandler(proxy.TransparentHandler(director)),
	)
}

func makeHttpOriginFunc() func(origin string) bool {
	return func(origin string) bool {
		return true
	}
}

func dialBackendOrFail(host string, codec grpc.Codec) *grpc.ClientConn {
	opt, err := NewConnOpts(host)
	if err != nil {
		log.Fatalf("failed dialing backend: %v", err)
	}

	opt = append(opt, grpc.WithDefaultCallOptions(grpc.CallCustomCodec(codec)))

	cc, err := grpc.Dial(host, opt...)
	if err != nil {
		log.Fatalf("failed dialing backend: %v", err)
	}
	return cc
}

func NewConnOpts(host string) ([]grpc.DialOption, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithAuthority(host))

	if strings.Contains(host, "localhost") {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		systemRoots, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
		cred := credentials.NewTLS(&tls.Config{
			RootCAs: systemRoots,
		})
		opts = append(opts, grpc.WithTransportCredentials(cred))
	}

	return opts, nil
}
