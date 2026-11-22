//go:build darwin

package media

import (
	"context"
	"iter"
	"math/bits"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

// Use the DNS-SD daemon on Apple platforms. Browsing a named Bonjour service
// uses the app's NSBonjourServices declaration in place of raw multicast.
func discoverFlymooLAN(ctx context.Context) iter.Seq[DiscoveredServer] {
	return func(yield func(DiscoveredServer) bool) {
		dns := flymooBonjour()
		if dns == nil || ctx.Err() != nil {
			return
		}
		events := new(flymooBonjourEvents)
		id := flymooBonjourID.Add(1)
		flymooBonjourSessions.Store(id, events)
		defer flymooBonjourSessions.Delete(id)
		var browse uintptr
		if dns.browse(&browse, 0, 0, flymooServiceType, "local.", dns.onBrowse, id) != 0 {
			return
		}
		defer dns.deallocate(browse)

		pending := make(map[uintptr]uintptr)
		seen := make(map[flymooBonjourInstance]bool)
		defer func() {
			for ref, key := range pending {
				dns.deallocate(ref)
				flymooBonjourSessions.Delete(key)
			}
		}()
		events.resolving = make(map[uintptr]string)
		for ctx.Err() == nil {
			refs := make([]uintptr, 0, 1+len(pending))
			refs = append(refs, browse)
			for ref := range pending {
				refs = append(refs, ref)
			}
			fds := make([]unix.PollFd, len(refs))
			for i, ref := range refs {
				fds[i] = unix.PollFd{Fd: dns.socket(ref), Events: unix.POLLIN}
			}
			_, err := unix.Poll(fds, 50)
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				return
			}
			for i, fd := range fds {
				if fd.Revents == 0 {
					continue
				}
				ref := refs[i]
				if ref == browse {
					if fd.Revents&unix.POLLIN == 0 || dns.process(ref) != 0 || events.failed {
						return
					}
				} else {
					if fd.Revents&unix.POLLIN != 0 {
						_ = dns.process(ref)
					}
					dns.deallocate(ref)
					delete(events.resolving, pending[ref])
					flymooBonjourSessions.Delete(pending[ref])
					delete(pending, ref)
				}
			}
			for _, instance := range events.instances {
				if seen[instance] || len(seen) >= 32 {
					continue
				}
				seen[instance] = true
				key := flymooBonjourID.Add(1)
				flymooBonjourSessions.Store(key, events)
				events.resolving[key] = instance.name
				var ref uintptr
				if dns.resolve(&ref, 0, instance.index, instance.name, instance.kind, instance.domain, dns.onResolve, key) == 0 {
					pending[ref] = key
				} else {
					delete(events.resolving, key)
					flymooBonjourSessions.Delete(key)
				}
			}
			events.instances = events.instances[:0]
			for _, server := range events.servers {
				if !yield(server) {
					return
				}
			}
			events.servers = events.servers[:0]
		}
	}
}

type flymooBonjourInstance struct {
	name, kind, domain string
	index              uint32
}

type flymooBonjourEvents struct {
	instances []flymooBonjourInstance
	servers   []DiscoveredServer
	resolving map[uintptr]string
	failed    bool
}

var flymooBonjourID atomic.Uintptr
var flymooBonjourSessions sync.Map

type flymooBonjourAPI struct {
	browse     func(*uintptr, uint32, uint32, string, string, uintptr, uintptr) int32
	resolve    func(*uintptr, uint32, uint32, string, string, string, uintptr, uintptr) int32
	socket     func(uintptr) int32
	process    func(uintptr) int32
	deallocate func(uintptr)
	onBrowse   uintptr
	onResolve  uintptr
}

var flymooBonjour = sync.OnceValue(func() *flymooBonjourAPI {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	api := new(flymooBonjourAPI)
	for _, symbol := range []struct {
		name   string
		target any
	}{
		{"DNSServiceBrowse", &api.browse}, {"DNSServiceResolve", &api.resolve},
		{"DNSServiceRefSockFD", &api.socket}, {"DNSServiceProcessResult", &api.process},
		{"DNSServiceRefDeallocate", &api.deallocate},
	} {
		address, err := purego.Dlsym(lib, symbol.name)
		if err != nil {
			_ = purego.Dlclose(lib)
			return nil
		}
		purego.RegisterFunc(symbol.target, address)
	}
	api.onBrowse = purego.NewCallback(flymooBonjourFound)
	api.onResolve = purego.NewCallback(flymooBonjourResolved)
	return api
})

func flymooBonjourFound(_ uintptr, flags, index uint32, status int32, name, kind, domain *byte, key uintptr) {
	value, exists := flymooBonjourSessions.Load(key)
	if !exists {
		return
	}
	events := value.(*flymooBonjourEvents)
	if status != 0 {
		events.failed = true
		return
	}
	const added = 2 // kDNSServiceFlagsAdd
	if flags&added != 0 && len(events.instances) < 32 {
		events.instances = append(events.instances, flymooBonjourInstance{bonjourString(name), bonjourString(kind), bonjourString(domain), index})
	}
}

func flymooBonjourResolved(_ uintptr, _, _ uint32, status int32, _, target *byte, port, _ uint16, _ *byte, key uintptr) {
	value, exists := flymooBonjourSessions.Load(key)
	if !exists {
		return
	}
	events := value.(*flymooBonjourEvents)
	host := bonjourString(target)
	if status == 0 && port != 0 && strings.HasSuffix(strings.ToLower(host), ".local.") {
		events.servers = append(events.servers, flymooDiscoveredServer(strings.TrimSuffix(host, "."), bits.ReverseBytes16(port), events.resolving[key]))
	}
}

func bonjourString(value *byte) string {
	if value == nil {
		return ""
	}
	size := 0
	for size < 1005 && *(*byte)(unsafe.Add(unsafe.Pointer(value), size)) != 0 {
		size++
	}
	return strings.Clone(unsafe.String(value, size))
}
