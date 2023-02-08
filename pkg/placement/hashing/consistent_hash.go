// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

// Package hashing Package placement is an implementation of Consistent Hashing and
// Consistent Hashing With Bounded Loads.
//
// https://en.wikipedia.org/wiki/Consistent_hashing
//
// https://research.googleblog.com/2017/04/consistent-hashing-with-bounded-loads.html
//
// https://github.com/lafikl/consistent/blob/master/consistent.go
//
package hashing

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	blake2b "github.com/minio/blake2b-simd" //使用带有SIMD指令的BLAKE2b的纯Go实现进行快速散列
	"github.com/pkg/errors"
)

//为了满足数据的高可用性，需要根据一些因子进行复制， 这些因子称为复制因子。
//假设我们集群的复制因子为2，那么属于'd'节点的数据将会被复制到按顺时针方向与之相隔最近的'b'和'e'节点上。
//这就保证了如果从'd'节点获取数据失败，这些数据能够从'b'或'e'节点获取。

var replicationFactor int //复制因子,

// ErrNoHosts 无主机错误
var ErrNoHosts = errors.New("no hosts added")

// ConsistentHashTables 是一个包含与给定版本一致的散列映射的表。
type ConsistentHashTables struct {
	Version string
	Entries map[string]*Consistent
}

// Host 主机信息
type Host struct {
	Name  string
	Port  int64
	Load  int64 // 负载
	AppID string
}

// Consistent 一致性哈希
type Consistent struct {
	hosts     map[uint64]string // 对 ip:port+i 进行的hash = ip:port
	sortedSet []uint64          // 对 ip:port+i 进行的hash    散列值[会重复replicationFactor]
	loadMap   map[string]*Host  // 主机名与主机实例的绑定    ip:port=Host{}
	totalLoad int64

	sync.RWMutex
}

// NewPlacementTables 返回具有给定版本的新的有状态放置表。
func NewPlacementTables(version string, entries map[string]*Consistent) *ConsistentHashTables {
	return &ConsistentHashTables{
		Version: version,
		Entries: entries,
	}
}

// NewHost 返回主机
func NewHost(name, id string, load int64, port int64) *Host {
	return &Host{
		Name:  name,
		Load:  load,
		Port:  port,
		AppID: id,
	}
}

// NewConsistentHash 返回一致性哈希
func NewConsistentHash() *Consistent {
	return &Consistent{
		hosts:     map[uint64]string{},
		sortedSet: []uint64{},
		loadMap:   map[string]*Host{},
	}
}

// NewFromExisting 使用已存在的值创建一个一致性哈希表
func NewFromExisting(hosts map[uint64]string, sortedSet []uint64, loadMap map[string]*Host) *Consistent {
	return &Consistent{
		hosts:     hosts,
		sortedSet: sortedSet,
		loadMap:   loadMap,
	}
}

// GetInternals 返回一致性哈希的内部数据
func (c *Consistent) GetInternals() (map[uint64]string, []uint64, map[string]*Host, int64) {
	c.RLock()
	defer c.RUnlock()

	return c.hosts, c.sortedSet, c.loadMap, c.totalLoad
}

// Add 添加一个主机
func (c *Consistent) Add(host, id string, port int64) bool {
	c.Lock()
	defer c.Unlock()

	if _, ok := c.loadMap[host]; ok {
		return true
	}

	c.loadMap[host] = &Host{Name: host, AppID: id, Load: 0, Port: port}
	for i := 0; i < replicationFactor; i++ {
		h := c.hash(fmt.Sprintf("%s%d", host, i))
		c.hosts[h] = host
		c.sortedSet = append(c.sortedSet, h)
	}
	// 对哈希值进行升序排序
	sort.Slice(c.sortedSet, func(i int, j int) bool {
		return c.sortedSet[i] < c.sortedSet[j]
	})

	return false
}

// Get 返回拥有“key”的主机。
//
// As described in https://en.wikipedia.org/wiki/Consistent_hashing
//
// 如果MAP中没有主机，则返回ErrNoHosts。
func (c *Consistent) Get(key string) (string, error) {
	c.RLock()
	defer c.RUnlock()

	if len(c.hosts) == 0 {
		return "", ErrNoHosts
	}

	h := c.hash(key)
	idx := c.search(h)
	return c.hosts[c.sortedSet[idx]], nil
}

// GetHost 获取主机
func (c *Consistent) GetHost(key string) (*Host, error) {
	h, err := c.Get(key)
	if err != nil {
		return nil, err
	}
	return c.loadMap[h], nil
}

// GetLeast uses Consistent Hashing With Bounded loads
//
// https://research.googleblog.com/2017/04/consistent-hashing-with-bounded-loads.html
//
// to pick the least loaded host that can serve the key
//
// It returns ErrNoHosts if the ring has no hosts in it.
//
func (c *Consistent) GetLeast(key string) (string, error) {
	c.RLock()
	defer c.RUnlock()

	if len(c.hosts) == 0 {
		return "", ErrNoHosts
	}

	h := c.hash(key)
	idx := c.search(h)

	i := idx
	for {
		host := c.hosts[c.sortedSet[i]]
		if c.loadOK(host) {
			return host, nil
		}
		i++
		if i >= len(c.hosts) {
			i = 0
		}
	}
}

// 在排序好的列表中寻找第一个>=key的值的下标
func (c *Consistent) search(key uint64) int {
	idx := sort.Search(len(c.sortedSet), func(i int) bool {
		return c.sortedSet[i] >= key
	})

	if idx >= len(c.sortedSet) {
		idx = 0
	}
	return idx
}

// UpdateLoad sets the load of `host` to the given `load`.
func (c *Consistent) UpdateLoad(host string, load int64) {
	c.Lock()
	defer c.Unlock()

	if _, ok := c.loadMap[host]; !ok {
		return
	}
	c.totalLoad -= c.loadMap[host].Load
	c.loadMap[host].Load = load
	c.totalLoad += load
}

// Inc increments the load of host by 1
//
// should only be used with if you obtained a host with GetLeast.
func (c *Consistent) Inc(host string) {
	c.Lock()
	defer c.Unlock()

	atomic.AddInt64(&c.loadMap[host].Load, 1)
	atomic.AddInt64(&c.totalLoad, 1)
}

// Done decrements the load of host by 1
//
// should only be used with if you obtained a host with GetLeast.
func (c *Consistent) Done(host string) {
	c.Lock()
	defer c.Unlock()

	if _, ok := c.loadMap[host]; !ok {
		return
	}
	atomic.AddInt64(&c.loadMap[host].Load, -1)
	atomic.AddInt64(&c.totalLoad, -1)
}

// Remove 移除主机
func (c *Consistent) Remove(host string) bool {
	c.Lock()
	defer c.Unlock()

	for i := 0; i < replicationFactor; i++ {
		h := c.hash(fmt.Sprintf("%s%d", host, i))
		delete(c.hosts, h)
		c.delSlice(h) // 从hashSet中删除某个指定的值
	}
	delete(c.loadMap, host)
	return true
}

// Hosts 返回主机列表
func (c *Consistent) Hosts() (hosts []string) {
	c.RLock()
	defer c.RUnlock()
	for k := range c.loadMap {
		hosts = append(hosts, k)
	}
	return hosts
}

// GetLoads returns the loads of all the hosts.
func (c *Consistent) GetLoads() map[string]int64 {
	loads := map[string]int64{}

	for k, v := range c.loadMap {
		loads[k] = v.Load
	}
	return loads
}

// MaxLoad returns the maximum load of the single host
// which is:
// (total_load/number_of_hosts)*1.25
// total_load = is the total number of active requests served by hosts
// for more info:
// https://research.googleblog.com/2017/04/consistent-hashing-with-bounded-loads.html
func (c *Consistent) MaxLoad() int64 {
	if c.totalLoad == 0 {
		c.totalLoad = 1
	}
	var avgLoadPerNode float64
	avgLoadPerNode = float64(c.totalLoad / int64(len(c.loadMap)))
	if avgLoadPerNode == 0 {
		avgLoadPerNode = 1
	}
	avgLoadPerNode = math.Ceil(avgLoadPerNode * 1.25)
	return int64(avgLoadPerNode)
}

func (c *Consistent) loadOK(host string) bool {
	// a safety check if someone performed c.Done more than needed
	if c.totalLoad < 0 {
		c.totalLoad = 0
	}

	var avgLoadPerNode float64
	avgLoadPerNode = float64((c.totalLoad + 1) / int64(len(c.loadMap)))
	if avgLoadPerNode == 0 {
		avgLoadPerNode = 1
	}
	avgLoadPerNode = math.Ceil(avgLoadPerNode * 1.25)

	bhost, ok := c.loadMap[host]
	if !ok {
		panic(fmt.Sprintf("given host(%s) not in loadsMap", bhost.Name))
	}

	if float64(bhost.Load)+1 <= avgLoadPerNode {
		return true
	}

	return false
}

// 从sortedSet删除指定值
func (c *Consistent) delSlice(val uint64) {
	// 为啥不使用sort.search code_debug/todo/todo.go:81
	idx := -1
	l := 0
	r := len(c.sortedSet) - 1
	for l <= r {
		m := (l + r) / 2
		if c.sortedSet[m] == val {
			idx = m
			break
		} else if c.sortedSet[m] < val {
			l = m + 1
		} else if c.sortedSet[m] > val {
			r = m - 1
		}
	}
	if idx != -1 {
		c.sortedSet = append(c.sortedSet[:idx], c.sortedSet[idx+1:]...)
	}
}

// 散列函数
func (c *Consistent) hash(key string) uint64 {
	out := blake2b.Sum512([]byte(key))
	return binary.LittleEndian.Uint64(out[:])
}

// SetReplicationFactor 设置复制因子
func SetReplicationFactor(factor int) {
	replicationFactor = factor
}
