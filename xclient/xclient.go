package xclient

import (
	"context"
	"io"
	"sync"
	. "GeeRpc"
)

type XClient struct {
	d Discovery
	mode SelectMode
	opt *Option
	mu sync.Mutex
	clients map[string]*Client
}

var _ io.Closer = (*XClient)(nil)


func NewXClient(d Discovery,mode SelectMode,opt *Option)*XClient{
	return &XClient{d:d,mode:mode,opt:opt,clients: make(map[string]*Client)}
}

func (xc *XClient)Close()error{
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key,client := range xc.clients{
		client.Close()
		delete(xc.clients,key)
	}
	return nil 
}

func (xc *XClient)dial(rpcAddr string)(*Client,error){
	xc.mu.Lock()
	defer xc.mu.Unlock()
	client,ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable(){
		client.Close()
		delete(xc.clients,rpcAddr)
		client = nil
	}
	if client == nil{
		var err error 
		client,err := XDial(rpcAddr,xc.opt)
		if err != nil{
			return nil,err 
		}
		xc.clients[rpcAddr] = client
	}
	return client,nil 
}

func (xc *XClient)call(rpcAddr string,ctx context.Context,serviceMethod string,args,reply interface{})error{
	client,err := xc.dial(rpcAddr)
	if err != nil{
		return err 
	}
	return client.Call(ctx,serviceMethod,args,reply)
}

func (xc *XClient)Call(ctx context.Context,serviceMethod string,args,reploy interface{})error{
	rpcAddr,err := xc.d.Get(xc.mode)
	if err != nil{
		return err 
	}
	return xc.call(rpcAddr,ctx,serviceMethod,args,reploy)
}