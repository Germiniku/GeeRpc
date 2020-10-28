package xclient

import (
	"errors"
	"math/rand"
	"sync"
	"time"
	"math"
)

type SelectMode int 

const (
	RandomSelect SelectMode = iota // select readonly 
	RoundRobinSelect 	// select using Robbin algorithm
)

var _ Discovery = (*MultiServersDiscovery)(nil)

type Discovery interface{
	Refresh() error 
	Update(servers []string)error 
	Get(mode SelectMode)(string,error)
	GetAll()([]string,error)
}

type MultiServersDiscovery struct {
	r *rand.Rand
	mu sync.Mutex
	servers []string
	index int  	
}

func NewMultiServersDiscovery(servers []string) *MultiServersDiscovery{
	d := &MultiServersDiscovery{
		servers: servers,
		r: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	d.index = d.r.Intn(math.MaxInt32-1)
	return d 
}

func (d *MultiServersDiscovery)Refresh()error{
	return nil 
}

func (d *MultiServersDiscovery)Update(servers []string)error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	return nil 
}

func (d *MultiServersDiscovery)Get(mode SelectMode)(string,error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.servers)
	if n == 0{
		return "",errors.New("rpc discovery: no available servers")
	}
	switch mode{
	case RandomSelect:
		return d.servers[d.r.Intn(n)],nil
	case RoundRobinSelect:
		s := d.servers[d.index % n]
		d.index = (d.index + 1) % n
		return s,nil
	default:
		return "",errors.New("rpc discovery: not supported select mode")
	}
}

func (d *MultiServersDiscovery)GetAll()([]string,error){
	d.mu.Lock()
	defer d.mu.Unlock()
	servers := make([]string,len(d.servers),len(d.servers))
	copy(servers,d.servers)
	return servers,nil
}