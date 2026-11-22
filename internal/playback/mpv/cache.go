//go:build (linux && !android) || (darwin && !ios) || windows

package mpv

import (
	"cmp"
	"math"
	"slices"
	"time"
	"unsafe"
)

const (
	formatNode      = 6
	formatNodeArray = 7
	formatNodeMap   = 8
)

// The first field is the union in libmpv's mpv_node. The list and its
// contents are owned by the returned node until mpv_free_node_contents.
type propertyNode struct {
	value  uint64
	format int32
}

type propertyList struct {
	count  int32
	values *propertyNode
	keys   **byte
}

func (node *propertyNode) list(format int32) *propertyList {
	if node == nil || node.format != format {
		return nil
	}
	list := *(**propertyList)(unsafe.Pointer(&node.value))
	if list == nil || list.values == nil || list.count <= 0 || list.count > 1024 {
		return nil
	}
	return list
}

func (node *propertyNode) field(name string) *propertyNode {
	list := node.list(formatNodeMap)
	if list == nil || list.keys == nil {
		return nil
	}
	keys := unsafe.Slice(list.keys, list.count)
	values := unsafe.Slice(list.values, list.count)
	for i, key := range keys {
		if readCString(key) == name {
			return &values[i]
		}
	}
	return nil
}

func (node *propertyNode) cacheTime() (time.Duration, bool) {
	if node == nil || node.format != formatDouble {
		return 0, false
	}
	nanos := math.Float64frombits(node.value) * float64(time.Second)
	if math.IsNaN(nanos) || nanos <= float64(math.MinInt64) || nanos >= float64(math.MaxInt64) {
		return 0, false
	}
	return time.Duration(nanos), true
}

func (p *backend) cacheRanges() []CacheRange {
	var root propertyNode
	if p.api.getProperty(p.movie, "demuxer-cache-state", formatNode, unsafe.Pointer(&root)) < 0 {
		return nil
	}
	defer p.api.freeNode(&root)
	list := root.field("seekable-ranges").list(formatNodeArray)
	if list == nil {
		return nil
	}
	ranges := make([]CacheRange, 0, list.count)
	nodes := unsafe.Slice(list.values, list.count)
	for i := range nodes {
		node := &nodes[i]
		start, validStart := node.field("start").cacheTime()
		end, validEnd := node.field("end").cacheTime()
		start = max(0, start)
		if validStart && validEnd && end > start {
			ranges = append(ranges, CacheRange{Start: start, End: end})
		}
	}
	return mergeCacheRanges(ranges)
}

// libmpv can report overlapping intervals while joining cached packets.
func mergeCacheRanges(ranges []CacheRange) []CacheRange {
	slices.SortFunc(ranges, func(a, b CacheRange) int { return cmp.Compare(a.Start, b.Start) })
	merged := ranges[:0]
	for _, current := range ranges {
		if n := len(merged); n > 0 && current.Start <= merged[n-1].End {
			merged[n-1].End = max(merged[n-1].End, current.End)
		} else {
			merged = append(merged, current)
		}
	}
	return merged
}
