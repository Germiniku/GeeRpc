package GeeRpc

import (
	"net/http"
	"GeeRpc/codec"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	connected        = "200 Connected to Gee RPC"
	defaultRPCPath   = "/_geerpc_"
	defaultDebugPath = "/debug/geerpc"
	MagicNumber      = 0x3bef5c
)

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GodType,
	ConnectTimeout: time.Second * 10,
}

var DefaultServer = NewServer()

var invaildRequest = struct{}{}

type Option struct {
	MagicNumber    int
	CodecType      codec.Type
	ConnectTimeout time.Duration
	HandleTimeout  time.Duration
}

type Server struct {
	serviceMap sync.Map
}

type request struct {
	H            *codec.Header
	argv, replyv reflect.Value
	mtype        *methodType
	svc          *service
}

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

func NewServer() *Server {
	return &Server{}
}

func Register(rcvr interface{}) error { return DefaultServer.Register(rcvr) }

func HandleHTTP(){
	DefaultServer.HandleHTTP()
}

func (server *Server)ServeHTTP(w http.ResponseWriter,req *http.Request){
	if req.Method != "CONNECT"{
		w.Header().Set("Content-Type","text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		io.WriteString(w,"405 MUST CONNECT")
		return 
	}
	// 劫持http连接
	conn,_,err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	server.ServeConn(conn)
}

func (server *Server)HandleHTTP(){
	http.Handle(defaultRPCPath,server)
	http.Handle(defaultDebugPath,debugHTTP{server})
	log.Println("rpc server debug path:",defaultDebugPath)
}

func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svcl, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can`t find service " + serviceName)
		return
	}
	svc = svcl.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server can`t find method " + methodName)
	}
	return
}

func (server *Server) Register(rcvc interface{}) error {
	s := newService(rcvc)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defined:" + s.name)
	}
	return nil
}

func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go server.ServeConn(conn)
	}
}

func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() { conn.Close() }()
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: option error:", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server invaild magic number: %x", opt.MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invaild codec type:%s", opt.CodecType)
		return
	}
	server.serveCodec(f(conn), &opt)
}

func (server *Server) serveCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.H.Error = err.Error()
			server.sendResponse(cc, req.H, invaildRequest, sending)
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{H: h}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, nil
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server:read argv err:", err)
	}
	return req, nil
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server:write response error:", err)
	}
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.H.Error = err.Error()
			server.sendResponse(cc, req.H, invaildRequest, sending)
			sent <- struct{}{}
			return
		}
		server.sendResponse(cc, req.H, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-time.After(timeout):
		req.H.Error = fmt.Sprintf("rpc server: request handle timeout:expect within %s", timeout)
	case <-called:
		<-sent
	}
}
