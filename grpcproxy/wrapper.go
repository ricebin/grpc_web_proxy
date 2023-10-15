package grpcproxy

import (
	"encoding/base64"
	"net/http"
	"strings"

	"google.golang.org/grpc"
)

// forked from https://github.com/improbable-eng/grpc-web/blob/master/go/grpcweb/wrapper.go

const grpcContentType = "application/grpc"
const grpcWebContentType = "application/grpc-web"
const grpcWebTextContentType = "application/grpc-web-text"

func hackIntoNormalGrpcRequest(req *http.Request) (*http.Request, bool) {
	// Hack, this should be a shallow copy, but let's see if this works
	req.ProtoMajor = 2
	req.ProtoMinor = 0

	contentType := req.Header.Get("content-type")
	incomingContentType := grpcWebContentType
	isTextFormat := strings.HasPrefix(contentType, grpcWebTextContentType)
	if isTextFormat {
		// body is base64-encoded: decode it; Wrap it in readerCloser so Body is still closed
		decoder := base64.NewDecoder(base64.StdEncoding, req.Body)
		req.Body = &readerCloser{reader: decoder, closer: req.Body}
		incomingContentType = grpcWebTextContentType
	}
	req.Header.Set("content-type", strings.Replace(contentType, incomingContentType, grpcContentType, 1))

	// Remove content-length header since it represents http1.1 payload size, not the sum of the h2
	// DATA frame payload lengths. https://http2.github.io/http2-spec/#malformed This effectively
	// switches to chunked encoding which is the default for h2
	req.Header.Del("content-length")

	return req, isTextFormat
}

func WrapServer(grpcServer *grpc.Server) http.Handler {
	wh := &wrappedServer{grpcServer: grpcServer}
	return wh
}

type wrappedServer struct {
	grpcServer *grpc.Server
}

func (w *wrappedServer) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	endpointFunc := func(req *http.Request) string {
		return req.URL.Path
	}
	intReq, isTextFormat := hackIntoNormalGrpcRequest(req)
	intResp := newGrpcWebResponse(resp, isTextFormat)
	req.URL.Path = endpointFunc(req)

	w.grpcServer.ServeHTTP(intResp, intReq)

	intResp.finishRequest(req)
}
